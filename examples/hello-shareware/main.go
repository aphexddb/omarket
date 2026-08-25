// Command hello-shareware is the demo app for omarchy-shareware: it proves
// the try-then-buy loop end to end. It always runs — an unregistered user
// just gets a friendly nag instead of a license ID. This is shareware, not a
// lock.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aphexddb/omarchy-shareware/license"
)

// appID matches catalog/hello-shareware.json's "id" field.
const appID = "hello-shareware"

// platformPublicKey is the platform's Ed25519 public key, base64 std-encoded
// (license.DecodePublicKey). Override at build time:
//
//	go build -ldflags "-X main.platformPublicKey=$PUBLIC" ./examples/hello-shareware
//
// The placeholder is intentionally invalid so an unconfigured build falls
// through to "unregistered."
var platformPublicKey = "REPLACE_WITH_PLATFORM_PUBLIC_KEY_BASE64"

const banner = `
 _          _ _         _
| |__   ___| | | ___    ___| |__   __ _ _ __ _____      ____ _ _ __ ___
| '_ \ / _ \ | |/ _ \  / __| '_ \ / _' | '__/ _ \ \ /\ / / _' | '__/ _ \
| | | |  __/ | | (_) | \__ \ | | | (_| | | |  __/\ V  V / (_| | | |  __/
|_| |_|\___|_|_|\___/  |___/_| |_|\__,_|_|  \___| \_/\_/ \__,_|_|  \___|
`

func main() {
	fmt.Print(banner)

	if lic := checkLicense(); lic != nil {
		fmt.Printf("registered to license %s\n", lic.ID)
	} else {
		fmt.Println("Unregistered — buy a key: omarket buy hello-shareware")
	}

	// The app works either way. Shareware, not a lock.
	fmt.Println("hello, shareware.")
}

// checkLicense looks for a stored license key and verifies it offline
// against the platform public key. Returns nil (not an error) for any
// unregistered case: no key on disk, unreadable key, malformed key, bad
// signature, or a misconfigured public key.
func checkLicense() *license.License {
	pub, err := license.DecodePublicKey(platformPublicKey)
	if err != nil {
		return nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	keyPath := filepath.Join(dir, "shareware", "licenses", appID+".key")

	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil
	}

	lic, err := license.Verify(string(raw), pub)
	if err != nil {
		return nil
	}
	return lic
}
