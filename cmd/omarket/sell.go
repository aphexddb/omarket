package main

import (
	"context"
	"flag"
	"fmt"
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
	sellPollInterval = 3 * time.Second
	sellPollTimeout  = 5 * time.Minute
)

func runSell(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: omarket sell <init|claim|push|testkey|status>")
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
	case "status":
		return runSellStatus(args[1:])
	default:
		return fmt.Errorf("usage: omarket sell <init|claim|push|testkey|status>")
	}
}

// runSellInit starts (or resumes reporting on) seller onboarding. If a
// seller token already exists, it does not create a second seller account;
// it just reports current status.
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
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	acct, err := c.CreateSeller(ctx)
	if err != nil {
		return fmt.Errorf("creating seller account: %w", err)
	}
	if err := client.SaveSellerToken(acct.SellerToken); err != nil {
		return fmt.Errorf("saving seller token: %w", err)
	}

	fmt.Println()
	fmt.Println(checkoutStyle.Render("Onboarding: " + acct.OnboardingURL))
	fmt.Println(mutedStyle.Render("Opening in your browser... (Ctrl-C to stop waiting; finish later and re-check with `omarket sell status`)"))
	openBrowser(acct.OnboardingURL)

	return pollSellerOnboarding(ctx, c, acct.SellerToken, acct.OnboardingURL)
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
		return
	}
	fmt.Println("apps:")
	for _, a := range me.Apps {
		listed := "unlisted"
		if a.Listed {
			listed = "listed"
		}
		fmt.Printf("  %-24s %-8s $%.2f\n", a.ID, listed, float64(a.PriceUSDCents)/100)
	}
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
	return nil
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

	if err := client.SaveLicense(id, key); err != nil {
		return fmt.Errorf("saving license: %w", err)
	}

	pub, err := resolvePublicKey()
	if err != nil {
		return err
	}
	lic, err := license.Verify(key, pub)
	if err != nil {
		return fmt.Errorf("verifying test license: %w", err)
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
