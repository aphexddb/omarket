package client

import (
	"crypto/ed25519"
	"fmt"

	"github.com/aphexddb/omarket/license"
)

// VerifyThenSaveLicense verifies key against pub *before* writing anything
// to disk, and only saves it (as app's stored license) once verification
// succeeds. This ordering matters: if whoever issued key signs with a
// different key than this build's resolved public key (e.g. a local dev
// stack with its own keypair), we must not leave an unverifiable license
// file behind for a future `omarket licenses` or app run to trip over.
//
// Shared by every path that lands a license on disk: `omarket buy`,
// `omarket sell testkey`, and the pending-purchase reconciler.
func VerifyThenSaveLicense(app, key string, pub ed25519.PublicKey) (*license.License, error) {
	lic, err := license.Verify(key, pub)
	if err != nil {
		return nil, fmt.Errorf("verifying license: %w (the signing key may not match this build's public key — e.g. a local stack; nothing was saved)", err)
	}
	if err := SaveLicense(app, key); err != nil {
		return nil, fmt.Errorf("saving license: %w", err)
	}
	return lic, nil
}
