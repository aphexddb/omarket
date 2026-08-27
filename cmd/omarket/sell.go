package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

const (
	sellPollInterval = 5 * time.Second
	sellPollTimeout  = 5 * time.Minute
)

func runSell(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: omarket sell <init|claim|push|testkey|payouts|status>")
	}
	switch args[0] {
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
	default:
		return fmt.Errorf("usage: omarket sell <init|claim|push|testkey|payouts|status>")
	}
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
			return fmt.Errorf("fetching seller status: %w", err)
		}
		printSellerStatus(me)
		printSellNextSteps()
		return nil
	}

	acct, err := c.CreateSeller(context.Background())
	if err != nil {
		return fmt.Errorf("creating seller account: %w", err)
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
// authenticated seller via POST /api/sellers/payouts. If the server hands
// back a fresh onboarding URL, it opens it in the browser and polls
// GET /api/sellers/me until charges are enabled (or the poll times out,
// which is not an error). If onboarding_url comes back empty, charges are
// already enabled. If the server has no Stripe configured, it returns a 503
// which is reported and treated as a non-fatal, informational exit.
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	acct, err := c.StartPayouts(ctx, token)
	if err != nil {
		var herr *client.HTTPError
		if errors.As(err, &herr) && herr.StatusCode == http.StatusServiceUnavailable {
			fmt.Println(errorStyle.Render("Payouts unavailable: " + herr.Message))
			fmt.Println(mutedStyle.Render("This server doesn't have payouts configured."))
			return nil
		}
		return fmt.Errorf("starting payouts: %w", err)
	}

	if acct.OnboardingURL == "" {
		fmt.Println(successStyle.Render("★ Payouts already set up — charges are enabled. ★"))
		return nil
	}

	fmt.Println()
	fmt.Println(checkoutStyle.Render("Onboarding: " + acct.OnboardingURL))
	fmt.Println(mutedStyle.Render("Opening in your browser... (Ctrl-C to stop waiting; finish later and re-check with `omarket sell status`)"))
	openBrowser(acct.OnboardingURL)

	return pollSellerOnboarding(ctx, c, token, acct.OnboardingURL)
}

// pollSellerOnboarding polls GET /api/sellers/me every sellPollInterval, up
// to sellPollTimeout, until charges_enabled is true. Timing out or being
// cancelled is not an error: it just prints how to finish and re-check
// later.
func pollSellerOnboarding(ctx context.Context, c *client.Client, token, onboardingURL string) error {
	deadline := time.Now().Add(sellPollTimeout)
	ticker := time.NewTicker(sellPollInterval)
	defer ticker.Stop()

	printLater := func() {
		fmt.Println(mutedStyle.Render("Finish onboarding at:"))
		fmt.Println(checkoutStyle.Render(onboardingURL))
		fmt.Println(mutedStyle.Render("Re-check any time with `omarket sell status`."))
	}

	frame := 0
	for {
		me, err := c.GetSellerMe(ctx, token)
		if err != nil {
			return fmt.Errorf("checking seller status: %w", err)
		}
		if me.ChargesEnabled {
			fmt.Print("\r")
			fmt.Println(successStyle.Render("★ Onboarding complete — you can now claim and sell apps. ★"))
			return nil
		}

		fmt.Printf("\r%s %s", string(spinnerFrames[frame%len(spinnerFrames)]), mutedStyle.Render("waiting for onboarding to complete..."))
		frame++

		if time.Now().After(deadline) {
			fmt.Println()
			fmt.Println(mutedStyle.Render("Still not enabled after " + sellPollTimeout.String() + "."))
			printLater()
			return nil
		}

		select {
		case <-ctx.Done():
			fmt.Println()
			printLater()
			return nil
		case <-ticker.C:
		}
	}
}

func runSellStatus(args []string) error {
	fs := flag.NewFlagSet("sell status", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))
	me, err := c.GetSellerMe(context.Background(), token)
	if err != nil {
		return fmt.Errorf("fetching seller status: %w", err)
	}
	printSellerStatus(me)
	return nil
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
			fmt.Printf("  %-24s %-8s $%.2f\n", a.ID, listed, float64(a.PriceUSDCents)/100)
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
	fs := flag.NewFlagSet("sell claim", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: omarket sell claim <app-id>")
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
		return fmt.Errorf("claiming %q: %w", id, err)
	}

	if err := client.WriteManifestTemplate(client.ManifestFilename, app.ID); err != nil {
		return err
	}

	fmt.Println(successStyle.Render("Claimed " + app.ID))
	fmt.Printf("Generated %s — edit it, then run `omarket sell push`.\n", client.ManifestFilename)
	if author := client.GitAuthor(); author != "" {
		fmt.Println(mutedStyle.Render("Pre-filled author from git config: " + author))
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
	if err := fs.Parse(args); err != nil {
		return err
	}

	m, err := client.ReadManifest(client.ManifestFilename)
	if err != nil {
		return fmt.Errorf("reading %s: %w (run `omarket sell claim <app-id>` first)", client.ManifestFilename, err)
	}
	if issues := client.ManifestIssues(m); len(issues) > 0 {
		fmt.Fprintf(os.Stderr, "refusing to push: %s still has template values:\n", client.ManifestFilename)
		for _, issue := range issues {
			fmt.Fprintln(os.Stderr, "  - "+issue)
		}
		return fmt.Errorf("edit %s and try again", client.ManifestFilename)
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))
	app, err := c.PushApp(context.Background(), token, m)
	if err != nil {
		return fmt.Errorf("pushing %s: %w", m.ID, err)
	}

	fmt.Println(successStyle.Render("Pushed " + app.ID))
	fmt.Printf("  name:     %s\n", app.Name)
	fmt.Printf("  price:    $%.2f\n", float64(app.PriceUSDCents)/100)
	fmt.Printf("  homepage: %s\n", app.Homepage)
	fmt.Println(mutedStyle.Render("Claimed and buyable by exact name; appearing in the browse catalog is curated by the platform."))

	// Best-effort payouts hint: never auto-launch the browser here — pushing
	// a manifest shouldn't pop a Stripe tab as a surprise side effect. If the
	// status check fails, just skip the hint rather than failing the push.
	if app.PriceUSDCents > 0 {
		if me, err := c.GetSellerMe(context.Background(), token); err == nil && !me.ChargesEnabled {
			fmt.Println(mutedStyle.Render("This app is priced but payouts aren't set up yet — run `omarket sell payouts` to get paid."))
		}
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
			return fmt.Errorf("reading %s: %w (pass an app id, or run this from a directory with a claimed app)", client.ManifestFilename, err)
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
		return fmt.Errorf("creating test license: %w", err)
	}

	pub, err := resolvePublicKey()
	if err != nil {
		return err
	}

	lic, err := verifyThenSaveLicense(id, key, pub)
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

// verifyThenSaveLicense verifies key against pub *before* writing anything
// to disk, and only saves it (as app's stored license) once verification
// succeeds. This ordering matters: if the server that issued key signs with
// a different key than this build's resolved public key (e.g. a local dev
// stack with its own keypair), we must not leave an unverifiable license
// file behind for a future `omarket licenses` or app run to trip over.
func verifyThenSaveLicense(app, key string, pub ed25519.PublicKey) (*license.License, error) {
	lic, err := license.Verify(key, pub)
	if err != nil {
		return nil, fmt.Errorf("verifying test license: %w (the server's signing key may not match this build's public key — e.g. a local stack; nothing was saved)", err)
	}
	if err := client.SaveLicense(app, key); err != nil {
		return nil, fmt.Errorf("saving license: %w", err)
	}
	return lic, nil
}

// openBrowser best-effort opens url in the platform default browser. Errors
// are ignored: the URL was already printed, so failing to auto-open is fine.
func openBrowser(url string) {
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
