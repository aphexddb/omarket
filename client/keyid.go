package client

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
)

// KeyID derives a short, stable identifier and a fingerprint for an Ed25519
// public key. keyID is "pk_" followed by the first 12 lowercase hex
// characters of sha256(raw public key bytes); fingerprint is
// "SHA256:<full lowercase hex digest>". Both are pure functions of the key
// bytes — nothing is stored — so the same key always yields the same id and
// fingerprint, whether it's the baked-in DefaultPublicKey or one fetched from
// a server's /api/pubkey.
func KeyID(pub ed25519.PublicKey) (keyID, fingerprint string) {
	sum := sha256.Sum256(pub)
	h := hex.EncodeToString(sum[:])
	return "pk_" + h[:12], "SHA256:" + h
}
