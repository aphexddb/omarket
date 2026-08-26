package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

func runLicenses(args []string) error {
	fs := flag.NewFlagSet("licenses", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := client.ListLicenses()
	if err != nil {
		return fmt.Errorf("listing licenses: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println("no licenses found")
		return nil
	}

	var pub ed25519.PublicKey
	if s := os.Getenv("SHAREWARE_PUBLIC_KEY"); s != "" {
		pub, err = license.DecodePublicKey(s)
		if err != nil {
			return fmt.Errorf("decoding SHAREWARE_PUBLIC_KEY: %w", err)
		}
	}

	for _, e := range entries {
		if pub == nil {
			fmt.Printf("%-24s %s\n", e.App, e.Path)
			continue
		}
		l, err := license.Verify(e.Key, pub)
		if err != nil {
			fmt.Printf("%-24s %-40s INVALID (%v)\n", e.App, e.Path, err)
			continue
		}
		fmt.Printf("%-24s %-40s VALID   %s\n", e.App, e.Path, l.ID)
	}
	return nil
}
