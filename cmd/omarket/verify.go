package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

// runVerify implements `omarket verify <license-key|path|-> [-server <url>]`.
// Verification is offline by default: it checks the key against
// SHAREWARE_PUBLIC_KEY (if set) or the baked-in client.DefaultPublicKey.
// With -server, it instead fetches that server's signing key(s) from
// GET /api/pubkey and verifies against those.
func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	server := fs.String("server", "", "verify against this server's key(s) (GET /api/pubkey) instead of the offline baked-in key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: omarket verify <license-key|path-to-key|-> [-server <url>]")
	}

	key, err := resolveLicenseArg(fs.Arg(0), os.Stdin)
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("no license key given")
	}

	if *server != "" {
		return verifyAgainstServer(context.Background(), key, *server)
	}
	return verifyAgainstBaked(key)
}

// resolveLicenseArg resolves the verify command's positional argument to a
// raw license key string:
//   - "-" reads and trims the key from stdin.
//   - an existing, readable file is read and trimmed (SHRW1 keys are stored
//     one per file under licenses/<app>.key).
//   - anything else is treated as the literal key (SHRW1 keys start with
//     "SHRW1.").
func resolveLicenseArg(arg string, stdin io.Reader) (string, error) {
	if arg == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		b, err := os.ReadFile(arg)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", arg, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(arg), nil
}

// verifyAgainstBaked verifies key fully offline against SHAREWARE_PUBLIC_KEY
// (if set) or the baked-in client.DefaultPublicKey.
func verifyAgainstBaked(key string) error {
	pub, err := resolvePublicKey()
	if err != nil {
		return err
	}

	lic, err := license.Verify(key, pub)
	if err != nil {
		return reportVerifyError(err)
	}

	keyID, _ := client.KeyID(pub)
	src := "baked-in"
	if os.Getenv("SHAREWARE_PUBLIC_KEY") != "" {
		src = "SHAREWARE_PUBLIC_KEY"
	}
	printVerifySuccess(lic, fmt.Sprintf("platform key %s (%s)", keyID, src))
	return nil
}

// verifyAgainstServer fetches server's signing key(s) from GET /api/pubkey
// and verifies key against every one of them until one matches, reporting
// the key_id that matched.
func verifyAgainstServer(ctx context.Context, key, server string) error {
	c := client.NewClient(server)
	entries, err := c.GetPublicKeys(ctx)
	if err != nil {
		return fmt.Errorf("fetching public key from %s: %w", server, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("%s returned no public keys", server)
	}

	var lastErr error
	for _, e := range entries {
		pub, decErr := license.DecodePublicKey(e.PublicKey)
		if decErr != nil {
			lastErr = fmt.Errorf("decoding key %s from %s: %w", e.KeyID, server, decErr)
			continue
		}

		lic, vErr := license.Verify(key, pub)
		if vErr == nil {
			keyID := e.KeyID
			if keyID == "" {
				keyID, _ = client.KeyID(pub)
			}
			printVerifySuccess(lic, fmt.Sprintf("platform key %s (from %s)", keyID, server))
			return nil
		}
		if errors.Is(vErr, license.ErrInvalidFormat) {
			// The key's format is independent of which signing key we try;
			// no other entry in entries will change this outcome.
			return reportVerifyError(vErr)
		}
		lastErr = vErr
	}
	if lastErr == nil {
		lastErr = license.ErrBadSignature
	}
	return reportVerifyError(lastErr)
}

// printVerifySuccess prints the rich success report: license id, app, kind
// (calling out a "test" kind distinctly), issued date, and which key
// verified it.
func printVerifySuccess(lic *license.License, signedBy string) {
	fmt.Println(successStyle.Render("valid") + " — signed by " + signedBy)
	fmt.Printf("  id:      %s\n", lic.ID)
	fmt.Printf("  app:     %s\n", lic.App)
	if lic.Kind == "test" {
		fmt.Println("  kind:    " + errorStyle.Render("TEST license — not a purchase"))
	} else {
		fmt.Printf("  kind:    %s\n", lic.Kind)
	}
	fmt.Printf("  issued:  %s\n", time.Unix(lic.IssuedAt, 0).UTC().Format("2006-01-02"))
}

// reportVerifyError turns a license verification error into a user-facing
// error that distinguishes a malformed key from one that failed its
// signature check.
func reportVerifyError(err error) error {
	switch {
	case errors.Is(err, license.ErrInvalidFormat):
		return fmt.Errorf("invalid license format: %w", err)
	case errors.Is(err, license.ErrBadSignature):
		return fmt.Errorf("signature verification failed: %w", err)
	default:
		return fmt.Errorf("verifying license: %w", err)
	}
}
