package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aphexddb/omarket/client"
)

// sellStatusWaitPerRequest and sellStatusWaitCap back `sell status -wait`:
// long-poll GET /api/sellers/me?wait=N, re-issuing until
// charges_enabled flips or the cap is reached. Vars, not consts, so tests
// can shrink them.
var (
	sellStatusWaitPerRequest = 25 * time.Second
	sellStatusWaitCap        = 2 * time.Minute
)

func runSell(args []string) error {
	if len(args) == 0 {
		if client.HasSellerToken() {
			return runSellStatus(nil)
		}
		printSellUsage()
		return nil
	}
	switch args[0] {
	case "-h", "--help", "help":
		printSellUsage()
		return nil
	case "init":
		return runSellInit(args[1:])
	case "claim":
		return runSellClaim(args[1:])
	case "push":
		return runSellPush(args[1:])
	case "testkey":
		return runSellTestkey(args[1:])
	case "payouts":
		return runSellPayouts(args[1:])
	case "status":
		return runSellStatus(args[1:])
	case "stats":
		return runSellStats(args[1:])
	default:
		printSellUsage()
		return fmt.Errorf("unknown sell command %q", args[0])
	}
}

// sellAppIDRule is the id constraint plus the reserved-name gotcha that
// ValidateAppID cannot check locally (the reserved list is server-side).
const sellAppIDRule = `app id: 3-64 characters, lowercase letters, digits, and hyphens
(no leading or trailing hyphen). Some names, including "omarket", are reserved.`

func printSellUsage() {
	fmt.Fprintln(os.Stderr, `usage: omarket sell <init|claim|push|testkey|payouts|status|stats>
  init                 start selling: instant, no Stripe needed
  claim <app-id>       claim an app id, writes ./omarket.json
  push                 push omarket.json (name/description/price)
  testkey [app]        mint yourself a local test license
  payouts              set up getting paid: Stripe onboarding in browser
  status               seller account + app status
  stats                licenses sold, per app

With no subcommand, prints this help, or seller status if a seller account already exists.

`+sellAppIDRule)
}

func printSellClaimUsage() {
	fmt.Fprintln(os.Stderr, "usage: omarket sell claim <app-id>")
	fmt.Fprintln(os.Stderr, sellAppIDRule)
}

func wantsHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// sellAPIError turns a failed seller API call into a sentence that does not
// leak method, path, or status code. Buy already does this; sell used to
// wrap *HTTPError and print "POST /api/apps: ... (status 409)".
func sellAPIError(action string, err error) error {
	var herr *client.HTTPError
	if !errors.As(err, &herr) {
		return fmt.Errorf("%s: %w", action, err)
	}
	msg := strings.TrimSpace(herr.Message)
	if msg == "" {
		msg = fmt.Sprintf("server returned HTTP %d", herr.StatusCode)
	}
	if advice := herr.Advice(); advice != "" {
		return fmt.Errorf("%s: %s — %s", action, msg, advice)
	}
	return fmt.Errorf("%s: %s", action, msg)
}

// claimError special-cases 409 so a reserved or taken id is a next step,
// not an HTTP dump. Other statuses go through sellAPIError.
func claimError(id string, err error) error {
	var herr *client.HTTPError
	if errors.As(err, &herr) && herr.StatusCode == http.StatusConflict {
		lower := strings.ToLower(herr.Message)
		switch {
		case strings.Contains(lower, "reserved"):
			return fmt.Errorf("%q is reserved by the platform; pick another id (3-64 characters, lowercase letters, digits, and hyphens)", id)
		case strings.Contains(lower, "taken") || strings.Contains(lower, "already"):
			return fmt.Errorf("%q is already claimed", id)
		default:
			return fmt.Errorf("%q is taken or reserved; pick another id", id)
		}
	}
	return sellAPIError(fmt.Sprintf("claiming %q", id), err)
}

func missingManifestError(err error, extra string) error {
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", client.ManifestFilename, err)
	}
	if extra != "" {
		return fmt.Errorf("no %s here — run `omarket sell claim <app-id>` first, or %s", client.ManifestFilename, extra)
	}
	return fmt.Errorf("no %s here — run `omarket sell claim <app-id>` first", client.ManifestFilename)
}

// runSellInit creates a seller account and saves its token (or reports the
// existing one if a token is already saved). It never touches Stripe — no
// browser launch, no polling. Getting paid is a separate, later step: see
// runSellPayouts.
func runSellInit(args []string) error {
	fs := flag.NewFlagSet("sell init", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))

	tok, err := client.LoadSellerToken()
	if err != nil {
		return fmt.Errorf("loading seller token: %w", err)
	}
	if tok != "" {
		fmt.Println(mutedStyle.Render("seller account already initialized; checking status..."))
		me, err := c.GetSellerMe(context.Background(), tok)
		if err != nil {
			return sellAPIError("fetching seller status", err)
		}
		printSellerStatus(me)
		printSellNextSteps()
		return nil
	}

	acct, err := c.CreateSeller(context.Background())
	if err != nil {
		return sellAPIError("creating seller account", err)
	}
	if err := client.SaveSellerToken(acct.SellerToken); err != nil {
		return fmt.Errorf("saving seller token: %w", err)
	}

	fmt.Println(successStyle.Render("★ Seller account created: " + acct.SellerID + " ★"))
	if path, err := client.SellerTokenPath(); err == nil {
		fmt.Println("Token written to " + path)
		fmt.Println(mutedStyle.Render("Back that file up. The server cannot restore it."))
	}
	printSellNextSteps()
	return nil
}

// printSellNextSteps prints the two things a freshly-initialized (or
// already-initialized) seller does next.
func printSellNextSteps() {
	fmt.Println()
	fmt.Println(mutedStyle.Render("Next steps:"))
	fmt.Println("  omarket sell claim <app-id>")
	fmt.Println(mutedStyle.Render("  omarket sell payouts   # when you're ready to get paid"))
}

// runSellPayouts starts (or resumes) Stripe Connect onboarding for the
// authenticated seller via POST /api/sellers/payouts, then exits. It never
// polls to wait for onboarding to finish: onboarding is a human filling in
// bank forms, sometimes over days, so a spinner was always the wrong shape.
// Check back later with `omarket sell status` (or `sell status -wait`). If
// onboarding_url comes back empty, charges are already enabled. If the
// server has no Stripe configured, it returns a 503 which is reported and
// treated as a non-fatal, informational exit.
func runSellPayouts(args []string) error {
	fs := flag.NewFlagSet("sell payouts", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))

	acct, err := c.StartPayouts(context.Background(), token)
	if err != nil {
		var herr *client.HTTPError
		if errors.As(err, &herr) && herr.StatusCode == http.StatusServiceUnavailable {
			fmt.Println(errorStyle.Render("Payouts unavailable: " + herr.Message))
			fmt.Println(mutedStyle.Render("This server doesn't have payouts configured."))
			return nil
		}
		return sellAPIError("starting payouts", err)
	}

	if acct.OnboardingURL == "" {
		fmt.Println(successStyle.Render("★ Payouts already set up - charges are enabled. ★"))
		return nil
	}

	fmt.Println()
	fmt.Println(checkoutStyle.Render("Onboarding: " + acct.OnboardingURL))
	fmt.Println(mutedStyle.Render("Opening in your browser..."))
	openBrowser(acct.OnboardingURL)
	fmt.Println(mutedStyle.Render("check with `omarket sell status` whenever you're done"))
	return nil
}

func runSellStatus(args []string) error {
	fs := flag.NewFlagSet("sell status", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	wait := fs.Bool("wait", false, fmt.Sprintf("long-poll for a status change, up to %s", sellStatusWaitCap))
	if err := fs.Parse(args); err != nil {
		return err
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))

	if !*wait {
		me, err := c.GetSellerMe(context.Background(), token)
		if err != nil {
			return sellAPIError("fetching seller status", err)
		}
		printSellerStatus(me)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	me, err := waitSellerStatus(ctx, c, token)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return sellAPIError("fetching seller status", err)
	}
	printSellerStatus(me)
	return nil
}

// waitSellerStatus long-polls GET /api/sellers/me?wait=N up to
// sellStatusWaitCap, using the shared cadence/429 handling so an old server
// (which ignores ?wait= and answers instantly) degrades to the same
// decay+jitter floor as everywhere else instead of a hot loop. It returns
// as soon as charges become enabled, or once the cap is reached — either
// way that's the caller's cue to print status, not an error.
func waitSellerStatus(ctx context.Context, c *client.Client, token string) (client.SellerMe, error) {
	deadline := time.Now().Add(sellStatusWaitCap)
	cd := &cadence{}

	waitNow := func() (client.SellerMe, error) {
		var me client.SellerMe
		_, _, err := pollRetrying(ctx, cd, func() (string, string, error) {
			var werr error
			me, werr = c.WaitSellerMe(ctx, token, sellStatusWaitPerRequest)
			return "", "", werr
		})
		return me, err
	}

	for {
		me, err := waitNow()
		if err != nil {
			return client.SellerMe{}, err
		}
		if me.ChargesEnabled || !time.Now().Before(deadline) {
			return me, nil
		}

		select {
		case <-time.After(cd.next()):
		case <-ctx.Done():
			return client.SellerMe{}, ctx.Err()
		}
	}
}

func printSellerStatus(me client.SellerMe) {
	fmt.Printf("seller:          %s\n", me.SellerID)
	fmt.Printf("charges enabled: %v\n", me.ChargesEnabled)
	if !me.ChargesEnabled && me.OnboardingURL != "" {
		fmt.Printf("finish onboarding: %s\n", me.OnboardingURL)
	}
	if len(me.Apps) == 0 {
		fmt.Println("apps:            (none claimed yet)")
	} else {
		fmt.Println("apps:")
		for _, a := range me.Apps {
			listed := "unlisted"
			if a.Listed {
				listed = "listed"
			}
			// formatPriceOrWare, not a raw dollar figure: a $0.00 here means
			// a ware-only listing, and naming the ware says what it asks for.
			fmt.Printf("  %-24s %-8s %s\n", cell(a.ID), listed,
				cell(formatPriceOrWare(int64(a.PriceUSDCents), a.Ware)))
		}
	}

	if !me.ChargesEnabled && me.OnboardingURL == "" && sellerHasPricedApp(me) {
		fmt.Println(mutedStyle.Render("One or more apps are priced but payouts aren't set up — run `omarket sell payouts` to get paid."))
	}
}

// sellerHasPricedApp reports whether any of the seller's apps have a
// nonzero price.
func sellerHasPricedApp(me client.SellerMe) bool {
	for _, a := range me.Apps {
		if a.PriceUSDCents > 0 {
			return true
		}
	}
	return false
}

func runSellClaim(args []string) error {
	if wantsHelpFlag(args) {
		printSellClaimUsage()
		return nil
	}
	fs := flag.NewFlagSet("sell claim", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		printSellClaimUsage()
		return fmt.Errorf("missing app id")
	}
	id := fs.Arg(0)
	if err := client.ValidateAppID(id); err != nil {
		return err
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))
	app, err := c.ClaimApp(context.Background(), token, id)
	if err != nil {
		return claimError(id, err)
	}

	// The author field is published. git config is asked for a suggestion,
	// but an email address is only used with the seller's explicit yes —
	// see resolveManifestAuthor for why the two candidate kinds are treated
	// differently.
	author, note := resolveManifestAuthor(newPrompter(), client.GitAuthorCandidate())

	if err := client.WriteManifestTemplate(client.ManifestFilename, app.ID, author); err != nil {
		return err
	}

	fmt.Println(successStyle.Render("Claimed " + app.ID))
	fmt.Printf("Generated %s — edit it, then run `omarket sell push`.\n", client.ManifestFilename)
	if note != "" {
		fmt.Println(mutedStyle.Render(note))
	}
	printWareSuggestions()
	return nil
}

// printWareSuggestions shows the "-ware" traditions a seller might pick for
// the manifest's ware field. It's a list of ideas, not a menu: the field is
// free-form, and inventing a new one is squarely in the spirit of it.
func printWareSuggestions() {
	fmt.Println()
	fmt.Println(headingStyle.Render("Pick your ware"))
	fmt.Println(mutedStyle.Render(fmt.Sprintf(
		"The %q field says what your app asks of people. Anything goes (%d chars max) —",
		"ware", client.MaxWareLen)))
	fmt.Println(mutedStyle.Render("these are just the well-worn ones:"))
	fmt.Println()
	for _, w := range client.WareSuggestions {
		fmt.Printf("  %s  %s\n", wareNameStyle.Render(pad(w.Name, 14)), mutedStyle.Render(w.Blurb))
	}
	fmt.Println()
	fmt.Println(mutedStyle.Render(`Then say it in "comment", e.g. "Buy me a beer if you like this tool. Cheers!"`))
	fmt.Println(mutedStyle.Render(fmt.Sprintf(
		`Set "price_usd_cents" to 0 and the ware is the whole ask — no payment, no Stripe. Otherwise it's $%.2f or more.`,
		float64(client.MinPriceUSDCents)/100)))
}

// pad right-pads s to width so the blurbs line up. Names are plain ASCII
// slugs, so a byte count is a column count here.
func pad(s string, width int) string {
	for len(s) < width {
		s += " "
	}
	return s
}

func runSellPush(args []string) error {
	fs := flag.NewFlagSet("sell push", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	assumeYes := fs.Bool("yes", false, "skip confirmation prompts (for scripts; implies consent to publish the author field as written)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	m, err := client.ReadManifest(client.ManifestFilename)
	if err != nil {
		return missingManifestError(err, "")
	}
	if issues := client.ManifestIssues(m); len(issues) > 0 {
		fmt.Fprintf(os.Stderr, "refusing to push: %s isn't ready:\n", client.ManifestFilename)
		for _, issue := range issues {
			fmt.Fprintln(os.Stderr, "  - "+issue)
		}
		return fmt.Errorf("edit %s and try again", client.ManifestFilename)
	}

	// Checked after the manifest is otherwise valid but before anything is
	// sent: this is the last point at which the email in the author field
	// is still private.
	if err := confirmPublishAuthor(newPrompter(), m.Author, *assumeYes); err != nil {
		return err
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))
	app, err := c.PushApp(context.Background(), token, m)
	if err != nil {
		return sellAPIError("pushing "+m.ID, err)
	}

	fmt.Println(successStyle.Render("Pushed " + app.ID))
	fmt.Printf("  name:     %s\n", app.Name)
	fmt.Printf("  price:    %s\n", formatPriceOrWare(int64(app.PriceUSDCents), app.Ware))
	fmt.Printf("  homepage: %s\n", app.Homepage)
	fmt.Println(mutedStyle.Render("Claimed and buyable by exact name; appearing in the browse catalog is curated by the platform."))

	if app.PriceUSDCents == 0 {
		// A ware-only listing has no Stripe leg, so the payouts nag below
		// would be noise. Say what the listing does ask for instead.
		fmt.Println(mutedStyle.Render(fmt.Sprintf(
			"Free — no payment, no Stripe needed. Buyers see your %s ask instead.",
			client.WareOrDefault(app.Ware))))
		return nil
	}

	// Best-effort payouts hint: never auto-launch the browser here — pushing
	// a manifest shouldn't pop a Stripe tab as a surprise side effect. If the
	// status check fails, just skip the hint rather than failing the push.
	if me, err := c.GetSellerMe(context.Background(), token); err == nil && !me.ChargesEnabled {
		fmt.Println(mutedStyle.Render("This app is priced but payouts aren't set up yet — run `omarket sell payouts` to get paid."))
	}
	return nil
}

func runSellTestkey(args []string) error {
	fs := flag.NewFlagSet("sell testkey", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	id := fs.Arg(0)
	if id == "" {
		m, err := client.ReadManifest(client.ManifestFilename)
		if err != nil {
			return missingManifestError(err, "pass the app id: `omarket sell testkey <app-id>`")
		}
		id = m.ID
	}
	if err := client.ValidateAppID(id); err != nil {
		return err
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))
	key, err := c.CreateTestLicense(context.Background(), token, id)
	if err != nil {
		return sellAPIError("creating test license", err)
	}

	pub, err := resolvePublicKey()
	if err != nil {
		return err
	}

	lic, err := client.VerifyThenSaveLicense(id, key, pub)
	if err != nil {
		return err
	}

	dir, _ := client.LicensesDir()
	path := filepath.Join(dir, id+".key")

	fmt.Println(successStyle.Render("Test license issued: " + lic.ID))
	fmt.Printf("License key saved to %s\n", path)
	fmt.Println("Your app now shows registered.")
	return nil
}

// openBrowser best-effort opens url in the platform default browser. Errors
// are ignored: the URL was already printed, so failing to auto-open is fine.
// A var, not a func, so tests can stub it out instead of actually spawning
// a browser process.
var openBrowser = defaultOpenBrowser

func defaultOpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return
	}
	_ = cmd.Start()
}

func requireSellerToken() (string, error) {
	token, err := client.LoadSellerToken()
	if err != nil {
		return "", fmt.Errorf("loading seller token: %w", err)
	}
	if token == "" {
		return "", fmt.Errorf("no seller account found; run `omarket sell init` first")
	}
	return token, nil
}
