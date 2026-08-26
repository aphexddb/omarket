package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/aphexddb/omarket/client"
	"github.com/mdp/qrterminal/v3"
)

const (
	pollInterval = 2 * time.Second
	pollTimeout  = 10 * time.Minute
)

var spinnerFrames = []rune{'|', '/', '-', '\\'}

func runBuy(args []string) error {
	fs := flag.NewFlagSet("buy", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	email := fs.String("email", "", "buyer email (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: omarket buy <app> [-email x]")
	}
	appID := fs.Arg(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c := client.NewClient(client.ResolveServer(*server))
	checkoutURL, token, err := c.Buy(ctx, appID, *email)
	if err != nil {
		return fmt.Errorf("starting purchase: %w", err)
	}

	fmt.Println()
	fmt.Println(checkoutStyle.Render("Checkout: " + checkoutURL))
	fmt.Println()
	qrterminal.GenerateHalfBlock(checkoutURL, qrterminal.M, os.Stdout)
	fmt.Println()
	fmt.Println(mutedStyle.Render("Waiting for payment... (Ctrl-C to cancel)"))

	key, err := pollUntilComplete(ctx, c, token)
	if err != nil {
		return err
	}

	if err := client.SaveLicense(appID, key); err != nil {
		return fmt.Errorf("saving license: %w", err)
	}

	dir, _ := client.LicensesDir()
	path := filepath.Join(dir, appID+".key")

	fmt.Println()
	fmt.Println(successStyle.Render("★ Registered! Thanks for supporting the developer. ★"))
	fmt.Printf("License key saved to %s\n", path)
	return nil
}

// pollUntilComplete polls /api/purchase/{token} every pollInterval, up to
// pollTimeout, showing a subtle spinner. It returns the license key once the
// purchase completes.
func pollUntilComplete(ctx context.Context, c *client.Client, token string) (string, error) {
	deadline := time.Now().Add(pollTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	frame := 0
	for {
		status, key, err := c.PollPurchase(ctx, token)
		if err != nil {
			return "", fmt.Errorf("polling purchase: %w", err)
		}
		if status == "complete" {
			fmt.Print("\r")
			return key, nil
		}

		fmt.Printf("\r%s %s", string(spinnerFrames[frame%len(spinnerFrames)]), mutedStyle.Render("waiting for payment..."))
		frame++

		if time.Now().After(deadline) {
			fmt.Println()
			return "", fmt.Errorf("timed out after %s waiting for payment", pollTimeout)
		}

		select {
		case <-ctx.Done():
			fmt.Println()
			return "", fmt.Errorf("cancelled")
		case <-ticker.C:
		}
	}
}
