// Package license implements the SHRW1 offline license key format:
// signing, verification, and keypair management for the shareware platform.
package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// License is the payload signed inside a SHRW1 key.
type License struct {
	V           int    `json:"v"`
	ID          string `json:"id"`
	App         string `json:"app"`
	EmailSHA256 string `json:"email_sha256"`
	IssuedAt    int64  `json:"issued_at"`
	Kind        string `json:"kind"`
}

// ErrInvalidFormat is returned when a key string doesn't parse as SHRW1.x.y.
var ErrInvalidFormat = errors.New("license: invalid key format")

// ErrBadSignature is returned when the ed25519 signature check fails.
var ErrBadSignature = errors.New("license: bad signature")

const keyPrefix = "SHRW1"

// GenerateKeypair returns a new ed25519 keypair.
func GenerateKeypair() (pub ed25519.PublicKey, priv ed25519.PrivateKey, err error) {
	return ed25519.GenerateKey(rand.Reader)
}

// EncodePrivateKey encodes priv as standard (padded) base64.
func EncodePrivateKey(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv)
}

// DecodePrivateKey decodes a standard (padded) base64 ed25519 private key.
func DecodePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return ed25519.PrivateKey(b), nil
}

// EncodePublicKey encodes pub as standard (padded) base64.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// DecodePublicKey decodes a standard (padded) base64 ed25519 public key.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return ed25519.PublicKey(b), nil
}

// HashEmail returns the hex sha256 of the lowercased, trimmed email, or ""
// for an empty (after trimming) email.
func HashEmail(email string) string {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(e))
	return hex.EncodeToString(sum[:])
}

// NewLicense builds a License for app/email/kind, filling in id (lic_ +
// 16 hex random chars), issued_at (now), and the hashed email.
func NewLicense(app, email, kind string) License {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return License{
		V:           1,
		ID:          "lic_" + hex.EncodeToString(b[:]),
		App:         app,
		EmailSHA256: HashEmail(email),
		IssuedAt:    time.Now().Unix(),
		Kind:        kind,
	}
}

// Sign serializes l to JSON and returns the SHRW1 key string, signed with priv.
func Sign(l License, priv ed25519.PrivateKey) (string, error) {
	payload, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, payload)
	return keyPrefix + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// Verify parses key, checks its signature against pub, and returns the
// decoded payload on success.
func Verify(key string, pub ed25519.PublicKey) (*License, error) {
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != keyPrefix {
		return nil, ErrInvalidFormat
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidFormat
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidFormat
	}
	if !ed25519.Verify(pub, payload, sig) {
		return nil, ErrBadSignature
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil {
		return nil, ErrInvalidFormat
	}
	if l.V != 1 {
		return nil, ErrInvalidFormat
	}
	return &l, nil
}
