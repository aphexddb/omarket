package client_test

import (
	"testing"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

// TestKeyIDFixedKey pins the derivation to a known input/output pair so a
// change in the algorithm (not just a change of key) fails this test. The
// expected values were computed independently (sha256 of the raw decoded
// key, formatted per KeyID's doc comment) rather than by calling KeyID
// itself.
func TestKeyIDFixedKey(t *testing.T) {
	pub, err := license.DecodePublicKey(client.DefaultPublicKey)
	if err != nil {
		t.Fatalf("DecodePublicKey: %v", err)
	}

	const wantKeyID = "pk_3caae11bf55a"
	const wantFingerprint = "SHA256:3caae11bf55a968b9d6dace234fbcfe4eb089c3589cac6fe0696a77f2e8a765f"

	keyID, fingerprint := client.KeyID(pub)
	if keyID != wantKeyID {
		t.Errorf("keyID = %q, want %q", keyID, wantKeyID)
	}
	if fingerprint != wantFingerprint {
		t.Errorf("fingerprint = %q, want %q", fingerprint, wantFingerprint)
	}
}

// TestKeyIDDeterministic checks the KeyID/fingerprint relationship holds for
// an arbitrary generated key too, not just the pinned default.
func TestKeyIDDeterministic(t *testing.T) {
	pub, _, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	keyID1, fp1 := client.KeyID(pub)
	keyID2, fp2 := client.KeyID(pub)
	if keyID1 != keyID2 || fp1 != fp2 {
		t.Fatal("KeyID is not deterministic for the same key")
	}
	if len(keyID1) != len("pk_")+12 {
		t.Errorf("keyID %q has unexpected length", keyID1)
	}
	if fp1[:7] != "SHA256:" {
		t.Errorf("fingerprint %q missing SHA256: prefix", fp1)
	}
}
