package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
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

func TestRunSellInitPrintsRealTokenPath(t *testing.T) {
	setConfigDir(t, t.TempDir())

	const secret = "st_secret_do_not_print"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sellers" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(client.SellerAccount{
			SellerID:    "sel_init_test",
			SellerToken: secret,
		})
	}))
	defer srv.Close()

	out := captureStdout(t, func() {
		if err := runSellInit([]string{"-server", srv.URL}); err != nil {
			t.Fatalf("runSellInit: %v", err)
		}
	})

	path, err := client.SellerTokenPath()
	if err != nil {
		t.Fatalf("SellerTokenPath: %v", err)
	}
	if !strings.Contains(out, path) {
		t.Fatalf("stdout missing real token path %q:\n%s", path, out)
	}
	if !strings.Contains(out, "Back that file up. The server cannot restore it.") {
		t.Fatalf("stdout missing backup warning:\n%s", out)
	}
	if !strings.Contains(out, "sel_init_test") {
		t.Fatalf("stdout missing seller id:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("stdout leaked the seller token:\n%s", out)
	}

	got, err := client.LoadSellerToken()
	if err != nil {
		t.Fatalf("LoadSellerToken: %v", err)
	}
	if got != secret {
		t.Fatalf("stored token = %q, want the secret on disk", got)
	}
}

func TestRunSellInitAlreadyInitializedSkipsBackupLecture(t *testing.T) {
	setConfigDir(t, t.TempDir())
	if err := client.SaveSellerToken("st_already"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sellers/me" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(client.SellerMe{SellerID: "sel_already"})
	}))
	defer srv.Close()

	out := captureStdout(t, func() {
		if err := runSellInit([]string{"-server", srv.URL}); err != nil {
			t.Fatalf("runSellInit: %v", err)
		}
	})
	if strings.Contains(out, "Back that file up") {
		t.Fatalf("repeat init should not re-lecture about backup:\n%s", out)
	}
	if !strings.Contains(out, "already initialized") {
		t.Fatalf("stdout missing already-initialized note:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	_ = r.Close()
	return buf.String()
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
