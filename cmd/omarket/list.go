package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aphexddb/omarket/client"
)

func priceString(a client.App) string {
	if a.Free() {
		return "FREE"
	}
	return fmt.Sprintf("$%.2f", float64(a.PriceCents)/100)
}

// printCatalog fetches and prints the full catalog as a plain table. It
// backs both `omarket buy` with no app argument and the hidden `omarket
// list` alias.
func printCatalog(server string) error {
	c := client.NewClient(client.ResolveServer(server))
	apps, err := c.GetCatalog(context.Background())
	if err != nil {
		return fmt.Errorf("fetching catalog: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPRICE\tWARE\tDESCRIPTION")
	for _, a := range apps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.ID, a.Name, priceString(a),
			client.WareOrDefault(a.Ware), a.Description)
	}
	return w.Flush()
}

// runList is a hidden backward-compat alias for `omarket buy` with no app
// argument (dropped from usage text, but kept working for anyone who
// scripted it).
func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return printCatalog(*server)
}

func findApp(apps []client.App, id string) (client.App, bool) {
	for _, a := range apps {
		if a.ID == id {
			return a, true
		}
	}
	return client.App{}, false
}

// runInfo is not one of the five approved top-level commands either, but
// nothing in the restructure calls for dropping its detail-view behavior, so
// like `list` and `install` it stays a hidden, working alias.
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
	fmt.Printf("  ware:     %s\n", client.WareOrDefault(a.Ware))
	if a.Author != "" {
		fmt.Printf("  author:   %s\n", a.Author)
	}
	fmt.Printf("  homepage: %s\n", a.Homepage)
	fmt.Printf("  source:   %s\n", a.Source)
	fmt.Printf("  package:  %s\n", a.Pkgname)
	if client.HasLicense(a.ID) {
		fmt.Println("  licensed: yes")
	}
	fmt.Println()
	fmt.Println(a.Description)
	if a.Comment != "" {
		fmt.Println()
		fmt.Println(wareBadge.Render(a.Comment))
	}
	return nil
}
