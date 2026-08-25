package main

import (
	"context"
	"flag"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/aphexddb/omarchy-shareware/client"
)

func runDev(args []string) error {
	if len(args) == 0 || args[0] != "onboard" {
		return fmt.Errorf("usage: omarket dev onboard -email x")
	}

	fs := flag.NewFlagSet("dev onboard", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	email := fs.String("email", "", "developer email")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" {
		return fmt.Errorf("-email is required")
	}

	c := client.NewClient(client.ResolveServer(*server))
	account, onboardingURL, err := c.DevOnboard(context.Background(), *email)
	if err != nil {
		return fmt.Errorf("onboarding: %w", err)
	}

	fmt.Printf("Stripe account:  %s\n", account)
	fmt.Printf("Onboarding URL:  %s\n", onboardingURL)
	openBrowser(onboardingURL)
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
