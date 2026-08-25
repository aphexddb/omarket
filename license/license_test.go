package license

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func mustKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	return pub, priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := mustKeypair(t)
	l := NewLicense("hello-shareware", "  User@Example.com ", "personal")

	key, err := Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.HasPrefix(key, "SHRW1.") {
		t.Fatalf("key missing SHRW1 prefix: %q", key)
	}

	got, err := Verify(key, pub)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ID != l.ID || got.App != l.App || got.EmailSHA256 != l.EmailSHA256 ||
		got.IssuedAt != l.IssuedAt || got.Kind != l.Kind || got.V != 1 {
		t.Fatalf("round-tripped license mismatch: got %+v, want %+v", got, l)
	}
}

func TestVerifyTamperedPayload(t *testing.T) {
	pub, priv := mustKeypair(t)
	l := NewLicense("hello-shareware", "user@example.com", "personal")
	key, err := Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected key shape: %q", key)
	}
	// flip the payload's leading char to corrupt it without breaking base64url.
	payload := []rune(parts[1])
	if payload[0] == 'a' {
		payload[0] = 'b'
	} else {
		payload[0] = 'a'
	}
	tampered := parts[0] + "." + string(payload) + "." + parts[2]

	if _, err := Verify(tampered, pub); err == nil {
		t.Fatal("Verify succeeded on tampered payload, want error")
	}
}

func TestVerifyWrongPublicKey(t *testing.T) {
	_, priv := mustKeypair(t)
	otherPub, _ := mustKeypair(t)
	l := NewLicense("hello-shareware", "user@example.com", "personal")

	key, err := Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if _, err := Verify(key, otherPub); err != ErrBadSignature {
		t.Fatalf("Verify with wrong pubkey: got %v, want ErrBadSignature", err)
	}
}

func TestVerifyMalformedKeys(t *testing.T) {
	pub, _ := mustKeypair(t)

	cases := []struct {
		name string
		key  string
	}{
		{"empty", ""},
		{"no dots", "SHRW1notakey"},
		{"wrong prefix", "SHRW2.abc.def"},
		{"too few parts", "SHRW1.abc"},
		{"too many parts", "SHRW1.abc.def.ghi"},
		{"bad base64 payload", "SHRW1.not!base64.abc"},
		{"bad base64 sig", "SHRW1.abc.not!base64"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Verify(tc.key, pub); err != ErrInvalidFormat {
				t.Fatalf("Verify(%q): got %v, want ErrInvalidFormat", tc.key, err)
			}
		})
	}
}

func TestHashEmailNormalization(t *testing.T) {
	base := HashEmail("user@example.com")

	cases := []string{
		"User@Example.com",
		"  user@example.com  ",
		"  USER@EXAMPLE.COM\t\n",
	}
	for _, in := range cases {
		if got := HashEmail(in); got != base {
			t.Errorf("HashEmail(%q) = %q, want %q", in, got, base)
		}
	}

	if got := HashEmail(""); got != "" {
		t.Errorf("HashEmail(\"\") = %q, want empty string", got)
	}
	if got := HashEmail("   \t  "); got != "" {
		t.Errorf("HashEmail(whitespace) = %q, want empty string", got)
	}
}

func TestKeypairEncodeDecodeRoundTrip(t *testing.T) {
	pub, priv := mustKeypair(t)

	pubStr := EncodePublicKey(pub)
	privStr := EncodePrivateKey(priv)

	gotPub, err := DecodePublicKey(pubStr)
	if err != nil {
		t.Fatalf("DecodePublicKey: %v", err)
	}
	if !gotPub.Equal(pub) {
		t.Fatal("decoded public key does not match original")
	}

	gotPriv, err := DecodePrivateKey(privStr)
	if err != nil {
		t.Fatalf("DecodePrivateKey: %v", err)
	}
	if !gotPriv.Equal(priv) {
		t.Fatal("decoded private key does not match original")
	}
}
