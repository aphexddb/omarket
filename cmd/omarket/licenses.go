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

	pub, err := resolvePublicKey()
	if err != nil {
		return err
	}

	for _, e := range entries {
		l, err := license.Verify(e.Key, pub)
		if err != nil {
			fmt.Printf("%-24s %-40s INVALID (%v)\n", e.App, e.Path, err)
			continue
		}
		fmt.Printf("%-24s %-40s VALID   %s\n", e.App, e.Path, l.ID)
	}
	return nil
}

// resolvePublicKey applies the license public key precedence:
// SHAREWARE_PUBLIC_KEY env (if set, for testing/local stacks) >
// client.DefaultPublicKey, the platform's baked-in key.
func resolvePublicKey() (ed25519.PublicKey, error) {
	s := os.Getenv("SHAREWARE_PUBLIC_KEY")
	src := "SHAREWARE_PUBLIC_KEY"
	if s == "" {
		s = client.DefaultPublicKey
		src = "default public key"
	}
	pub, err := license.DecodePublicKey(s)
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", src, err)
	}
	return pub, nil
}
