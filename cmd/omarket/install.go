package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/aphexddb/omarket/client"
)

// runInstall shells out to install an app's underlying package (pacman,
// falling back to yay, or printing the command if neither is found; see
// client.Install). It's a buyer-facing action distinct from `buy` — `buy`
// only handles payment and the license key, while this installs the
// software itself, and the TUI's "i" key drives it independently of "b" buy.
// It doesn't fit any of the five top-level commands, so it stays a hidden,
// working alias (not in usage text) rather than being folded or dropped.
func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: omarket install <app>")
	}
	appID := fs.Arg(0)

	c := client.NewClient(client.ResolveServer(*server))
	apps, err := c.GetCatalog(context.Background())
	if err != nil {
		return fmt.Errorf("fetching catalog: %w", err)
	}

	a, ok := findApp(apps, appID)
	if !ok {
		return fmt.Errorf("app %q not found in catalog", appID)
	}

	msg, err := client.Install(nil, a.Pkgname)
	if err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}
