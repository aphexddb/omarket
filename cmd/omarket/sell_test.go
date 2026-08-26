package main

import (
	"errors"
	"runtime"
	"testing"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

// setConfigDir points os.UserConfigDir() at dir for the duration of the
// test, matching the helper used by client/config_test.go.
func setConfigDir(t *testing.T, dir string) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
}

func TestVerifyThenSaveLicense(t *testing.T) {
	t.Run("verified key is saved", func(t *testing.T) {
		setConfigDir(t, t.TempDir())

		pub, priv, err := license.GenerateKeypair()
		if err != nil {
			t.Fatalf("GenerateKeypair: %v", err)
		}
		l := license.NewLicense("app1", "buyer@example.com", "test")
		key, err := license.Sign(l, priv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}

		got, err := verifyThenSaveLicense("app1", key, pub)
		if err != nil {
			t.Fatalf("verifyThenSaveLicense: %v", err)
		}
		if got.ID != l.ID {
			t.Fatalf("got license ID %q, want %q", got.ID, l.ID)
		}

		if !client.HasLicense("app1") {
			t.Fatal("expected license to be saved after successful verification")
		}
		saved, err := client.LoadLicense("app1")
		if err != nil {
			t.Fatalf("LoadLicense: %v", err)
		}
		if saved != key {
			t.Fatalf("saved license key %q, want %q", saved, key)
		}
	})

	t.Run("verification failure saves nothing", func(t *testing.T) {
		setConfigDir(t, t.TempDir())

		// Sign with one keypair but verify against another, simulating a
		// server whose signing key doesn't match this build's public key
		// (e.g. a local stack).
		_, priv, err := license.GenerateKeypair()
		if err != nil {
			t.Fatalf("GenerateKeypair: %v", err)
		}
		otherPub, _, err := license.GenerateKeypair()
		if err != nil {
			t.Fatalf("GenerateKeypair: %v", err)
		}
		l := license.NewLicense("app2", "buyer@example.com", "test")
		key, err := license.Sign(l, priv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}

		_, err = verifyThenSaveLicense("app2", key, otherPub)
		if err == nil {
			t.Fatal("verifyThenSaveLicense: expected error for mismatched public key, got nil")
		}
		if !errors.Is(err, license.ErrBadSignature) {
			t.Fatalf("expected error wrapping license.ErrBadSignature, got: %v", err)
		}

		if client.HasLicense("app2") {
			t.Fatal("verifyThenSaveLicense must not save a license that failed verification")
		}
	})
}
