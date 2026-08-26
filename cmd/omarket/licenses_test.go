package main

import (
	"encoding/base64"
	"testing"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

func TestResolvePublicKey(t *testing.T) {
	t.Run("env set wins", func(t *testing.T) {
		pub, _, err := license.GenerateKeypair()
		if err != nil {
			t.Fatalf("GenerateKeypair: %v", err)
		}
		encoded := license.EncodePublicKey(pub)
		t.Setenv("SHAREWARE_PUBLIC_KEY", encoded)

		got, err := resolvePublicKey()
		if err != nil {
			t.Fatalf("resolvePublicKey: %v", err)
		}
		if base64.StdEncoding.EncodeToString(got) != encoded {
			t.Fatalf("got %q, want env-provided key %q", base64.StdEncoding.EncodeToString(got), encoded)
		}
	})

	t.Run("unset falls back to default", func(t *testing.T) {
		t.Setenv("SHAREWARE_PUBLIC_KEY", "")

		got, err := resolvePublicKey()
		if err != nil {
			t.Fatalf("resolvePublicKey: %v", err)
		}
		if base64.StdEncoding.EncodeToString(got) != client.DefaultPublicKey {
			t.Fatalf("got %q, want default key %q", base64.StdEncoding.EncodeToString(got), client.DefaultPublicKey)
		}
	})

	t.Run("garbage env errors", func(t *testing.T) {
		t.Setenv("SHAREWARE_PUBLIC_KEY", "not-valid-base64!!!")

		if _, err := resolvePublicKey(); err == nil {
			t.Fatal("resolvePublicKey: expected error for invalid SHAREWARE_PUBLIC_KEY, got nil")
		}
	})
}
