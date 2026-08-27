// Command gen regenerates the demo fixtures in examples/testdata: a throwaway
// Ed25519 keypair, a valid SHRW1 license for hello-shareware, a license for a
// different app, and a tampered key. They let every example in examples/ run
// end to end offline, with no purchase and no server.
//
// The demo keypair is NOT the omarket platform key. It exists so the examples
// have something to verify against; nothing signed by it is worth anything.
//
//	go run ./examples/testdata/gen
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aphexddb/omarket/license"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := outDir()
	if err != nil {
		return err
	}

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		return err
	}

	// A real purchase: kind "personal", app hello-shareware.
	good := license.NewLicense("hello-shareware", "buyer@example.com", "personal")
	goodKey, err := license.Sign(good, priv)
	if err != nil {
		return err
	}

	// Correctly signed, but for a different app. An app that checks only the
	// signature and not the app id would unlock on this — the examples don't.
	other := license.NewLicense("some-other-app", "buyer@example.com", "personal")
	otherKey, err := license.Sign(other, priv)
	if err != nil {
		return err
	}

	files := map[string]string{
		"demo.pub":            license.EncodePublicKey(pub),
		"demo.secret":         license.EncodePrivateKey(priv),
		"hello-shareware.key": goodKey,
		"other-app.key":       otherKey,
		"tampered.key":        tamper(goodKey),
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}

// tamper flips one character of the payload segment, leaving the signature
// intact — the shape of a key someone edited by hand to promote themselves
// from "personal" to "team". The signature no longer matches the bytes.
func tamper(key string) string {
	parts := strings.SplitN(key, ".", 3)
	payload := []byte(parts[1])
	if payload[0] == 'A' {
		payload[0] = 'B'
	} else {
		payload[0] = 'A'
	}
	return parts[0] + "." + string(payload) + "." + parts[2]
}

// outDir returns examples/testdata relative to this source file's package, so
// the generator works from any working directory.
func outDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// go run ./examples/testdata/gen leaves us at the repo root.
	for _, cand := range []string{
		filepath.Join(wd, "examples", "testdata"),
		filepath.Join(wd, ".."),
	} {
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("cannot locate examples/testdata from %s", wd)
}
