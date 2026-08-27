// Command hello-shareware is a complete, minimal shareware app.
//
// It looks for a license key on disk, verifies it offline against the omarket
// platform public key, and unlocks a feature when the key is genuine and
// belongs to this app. There is no account, no activation call, and no
// phone-home: everything below runs with the network unplugged.
//
//	go run ./examples/go
//	go run ./examples/go /path/to/some.key
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aphexddb/omarket/license"
)

const (
	// appID must match the "app" field inside the license payload. This is
	// what stops a valid key for someone else's app from unlocking yours.
	appID   = "hello-shareware"
	appName = "Hello Shareware"

	// platformPublicKey is omarket's Ed25519 license-signing key, also served
	// at https://omarket.dev/api/pubkey. Bake it into your binary — that is
	// what makes verification offline. It is a public key: shipping it to
	// users is the point, and it cannot be used to mint licenses.
	platformPublicKey = "vIODssCr6I8zWUTaK/IhdHzkoK5LTdEzbtIeXRnatko="
)

func main() {
	fmt.Printf("%s 1.0\n\n", appName)

	path := defaultLicensePath()
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	lic, err := checkLicense(path)
	if err != nil {
		fmt.Printf("  [ ] unregistered — %v\n", err)
		fmt.Printf("      buy a key:  omarket buy %s\n\n", appID)
		fmt.Println("  the deluxe feature is off until this app is registered.")
		return
	}

	fmt.Println("  [x] registered")
	fmt.Printf("      license  %s\n", lic.ID)
	fmt.Printf("      kind     %s\n", lic.Kind)
	fmt.Printf("      issued   %s\n\n", time.Unix(lic.IssuedAt, 0).UTC().Format("2006-01-02"))
	fmt.Println("  the deluxe feature is unlocked. Enjoy.")
}

// checkLicense reads the key at path and decides whether this app is
// registered. Three things have to hold, and all three matter:
//
//  1. the Ed25519 signature checks out against the platform public key —
//     license.Verify does this, over the exact payload bytes;
//  2. the payload is format v1 — also license.Verify;
//  3. the payload's app id is ours — that one is on you.
//
// Skip (3) and any paid omarket license, for any app, unlocks yours.
func checkLicense(path string) (*license.License, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no license file at %s", path)
	}
	if err != nil {
		return nil, err
	}

	pub, err := license.DecodePublicKey(publicKey())
	if err != nil {
		return nil, fmt.Errorf("bad public key: %w", err)
	}

	lic, err := license.Verify(strings.TrimSpace(string(raw)), pub)
	if err != nil {
		// Two failures worth telling apart, and errors.Is sees through the
		// wrapping to say which: a file that was never a SHRW1 key (a bad
		// paste, a truncated download) versus one somebody edited.
		switch {
		case errors.Is(err, license.ErrInvalidFormat):
			return nil, errors.New("not a SHRW1 key")
		case errors.Is(err, license.ErrBadSignature):
			return nil, errors.New("bad signature")
		default:
			return nil, err
		}
	}
	if lic.App != appID {
		return nil, fmt.Errorf("key is for %q, not %q", lic.App, appID)
	}
	return lic, nil
}

// publicKey returns the signing key to verify against. SHAREWARE_PUBLIC_KEY
// overrides the baked-in platform key, which is how you test against a local
// stack or the demo keypair in examples/testdata.
func publicKey() string {
	if k := os.Getenv("SHAREWARE_PUBLIC_KEY"); k != "" {
		return k
	}
	return platformPublicKey
}

// defaultLicensePath is where `omarket buy` writes the key:
// os.UserConfigDir()/shareware/licenses/<app>.key — on Linux that is
// $XDG_CONFIG_HOME (or ~/.config) /shareware/licenses/<app>.key.
func defaultLicensePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return appID + ".key"
	}
	return filepath.Join(dir, "shareware", "licenses", appID+".key")
}
