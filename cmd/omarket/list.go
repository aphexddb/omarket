package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aphexddb/omarchy-shareware/client"
)

func priceString(a client.App) string {
	if a.Free() {
		return "FREE"
	}
	return fmt.Sprintf("$%.2f", float64(a.PriceCents)/100)
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))
	apps, err := c.GetCatalog(context.Background())
	if err != nil {
		return fmt.Errorf("fetching catalog: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPRICE\tDESCRIPTION")
	for _, a := range apps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, a.Name, priceString(a), a.Description)
	}
	return w.Flush()
}

func findApp(apps []client.App, id string) (client.App, bool) {
	for _, a := range apps {
		if a.ID == id {
			return a, true
		}
	}
	return client.App{}, false
}

func runInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: omarket info <app>")
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

	fmt.Printf("%s (%s)\n", a.Name, a.ID)
	fmt.Printf("  version:  %s\n", a.Version)
	fmt.Printf("  price:    %s\n", priceString(a))
	fmt.Printf("  kind:     %s\n", a.Kind)
	fmt.Printf("  homepage: %s\n", a.Homepage)
	fmt.Printf("  source:   %s\n", a.Source)
	fmt.Printf("  package:  %s\n", a.Pkgname)
	if client.HasLicense(a.ID) {
		fmt.Println("  licensed: yes")
	}
	fmt.Println()
	fmt.Println(a.Description)
	return nil
}
