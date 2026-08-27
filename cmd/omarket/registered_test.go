package main

import (
	"testing"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

func TestSelfRegistered(t *testing.T) {
	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	save := func(t *testing.T, filenameApp, payloadApp, kind, keyOverride string) {
		t.Helper()
		setConfigDir(t, t.TempDir())
		t.Setenv("SHAREWARE_PUBLIC_KEY", license.EncodePublicKey(pub))
		if keyOverride != "" {
			if err := client.SaveLicense(filenameApp, keyOverride); err != nil {
				t.Fatalf("SaveLicense: %v", err)
			}
			return
		}
		if filenameApp == "" {
			return
		}
		l := license.NewLicense(payloadApp, "buyer@example.com", kind)
		key, err := license.Sign(l, priv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if err := client.SaveLicense(filenameApp, key); err != nil {
			t.Fatalf("SaveLicense: %v", err)
		}
	}

	t.Run("no key", func(t *testing.T) {
		save(t, "", "", "", "")
		if selfRegistered() {
			t.Fatal("want unregistered")
		}
	})

	t.Run("wrong app id in payload", func(t *testing.T) {
		save(t, selfAppID, "hello-shareware", "personal", "")
		if selfRegistered() {
			t.Fatal("want unregistered")
		}
	})

	t.Run("garbage key", func(t *testing.T) {
		save(t, selfAppID, "", "", "SHRW1.not.a-key")
		if selfRegistered() {
			t.Fatal("want unregistered")
		}
	})

	t.Run("payload stored under another filename", func(t *testing.T) {
		save(t, "hello-shareware", selfAppID, "personal", "")
		if selfRegistered() {
			t.Fatal("want unregistered")
		}
	})

	for _, kind := range []string{"personal", "ware", "team", "test"} {
		t.Run("kind "+kind, func(t *testing.T) {
			save(t, selfAppID, selfAppID, kind, "")
			if !selfRegistered() {
				t.Fatal("want registered")
			}
		})
	}
}
