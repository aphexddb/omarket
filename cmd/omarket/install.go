package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/aphexddb/omarchy-shareware/client"
)

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
