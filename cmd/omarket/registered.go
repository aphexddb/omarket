package main

import (
	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

// selfAppID is omarket's own listing. A SHRW1 key for any other app still
// carries a valid platform signature; skip the app-id check and any paid
// license unlocks the thank-you.
const selfAppID = "omarket"

const thanksForTheBeer = "thank you for the beer money!"

// selfRegistered is the same check examples/ teach, applied to this binary:
// read the key, Ed25519-verify over the decoded payload bytes, require
// v==1 and app=="omarket". SHAREWARE_PUBLIC_KEY overrides the baked-in
// platform key. Unregistered is not an error; the TUI just withholds the
// thank-you.
func selfRegistered() bool {
	raw, err := client.LoadLicense(selfAppID)
	if err != nil {
		return false
	}
	pub, err := resolvePublicKey()
	if err != nil {
		return false
	}
	lic, err := license.Verify(raw, pub)
	return err == nil && lic.App == selfAppID
}
