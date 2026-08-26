package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/license"
)

// --- resolveLicenseArg: arg vs. file vs. stdin resolution ---

func TestResolveLicenseArgRawKey(t *testing.T) {
	got, err := resolveLicenseArg("SHRW1.abc.def", strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveLicenseArg: %v", err)
	}
	if got != "SHRW1.abc.def" {
		t.Fatalf("got %q, want raw key unchanged", got)
	}
}

func TestResolveLicenseArgFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.key")
	if err := os.WriteFile(path, []byte("SHRW1.fromfile.sig\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := resolveLicenseArg(path, strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveLicenseArg: %v", err)
	}
	if got != "SHRW1.fromfile.sig" {
		t.Fatalf("got %q, want trimmed file contents", got)
	}
}

func TestResolveLicenseArgStdin(t *testing.T) {
	got, err := resolveLicenseArg("-", strings.NewReader("  SHRW1.fromstdin.sig\n"))
	if err != nil {
		t.Fatalf("resolveLicenseArg: %v", err)
	}
	if got != "SHRW1.fromstdin.sig" {
		t.Fatalf("got %q, want trimmed stdin contents", got)
	}
}

func TestResolveLicenseArgNonexistentPathTreatedAsKey(t *testing.T) {
	// A path that doesn't exist on disk isn't an error here — it's just
	// treated as the literal key string (which will then fail format
	// validation downstream, not here).
	got, err := resolveLicenseArg("/no/such/file/SHRW1.x.y", strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveLicenseArg: %v", err)
	}
	if got != "/no/such/file/SHRW1.x.y" {
		t.Fatalf("got %q, want the argument treated literally", got)
	}
}

// --- verifyAgainstServer: multi-key verification against /api/pubkey ---

func newTestLicense(t *testing.T) (key string, pubB64 string) {
	t.Helper()
	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	l := license.NewLicense("hello-shareware", "buyer@example.com", "personal")
	key, err = license.Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return key, license.EncodePublicKey(pub)
}

func TestVerifyAgainstServerCurrentShapeMultiKey(t *testing.T) {
	key, pubB64 := newTestLicense(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pubkey" {
			http.NotFound(w, r)
			return
		}
		// The matching key is second in the list — exercises "try every
		// entry until one matches".
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{"key_id": "pk_wrongwrong", "algorithm": "ed25519", "public_key": "not-the-right-key-base64-------"},
				{"key_id": "pk_correct0000", "algorithm": "ed25519", "public_key": pubB64},
			},
		})
	}))
	defer srv.Close()

	if err := verifyAgainstServer(context.Background(), key, srv.URL); err != nil {
		t.Fatalf("verifyAgainstServer: %v", err)
	}
}

func TestVerifyAgainstServerLegacyShape(t *testing.T) {
	key, pubB64 := newTestLicense(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pubkey" {
			http.NotFound(w, r)
			return
		}
		// Legacy shape: only the top-level public_key, no "keys" array.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"public_key": pubB64,
		})
	}))
	defer srv.Close()

	if err := verifyAgainstServer(context.Background(), key, srv.URL); err != nil {
		t.Fatalf("verifyAgainstServer: %v", err)
	}
}

func TestVerifyAgainstServerNoMatchingKey(t *testing.T) {
	key, _ := newTestLicense(t)
	_, otherPubB64 := newTestLicense(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"public_key": otherPubB64,
		})
	}))
	defer srv.Close()

	err := verifyAgainstServer(context.Background(), key, srv.URL)
	if err == nil {
		t.Fatal("verifyAgainstServer: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("error = %v, want signature verification failure", err)
	}
}

func TestVerifyAgainstServerBadFormat(t *testing.T) {
	_, pubB64 := newTestLicense(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"public_key": pubB64,
		})
	}))
	defer srv.Close()

	err := verifyAgainstServer(context.Background(), "not-a-shrw1-key", srv.URL)
	if err == nil {
		t.Fatal("verifyAgainstServer: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid license format") {
		t.Fatalf("error = %v, want invalid format", err)
	}
}

// --- verifyAgainstBaked: offline verification precedence ---

func TestVerifyAgainstBakedEnvOverride(t *testing.T) {
	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	t.Setenv("SHAREWARE_PUBLIC_KEY", license.EncodePublicKey(pub))

	l := license.NewLicense("hello-shareware", "buyer@example.com", "personal")
	key, err := license.Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if err := verifyAgainstBaked(key); err != nil {
		t.Fatalf("verifyAgainstBaked: %v", err)
	}
}

func TestVerifyAgainstBakedBadSignature(t *testing.T) {
	t.Setenv("SHAREWARE_PUBLIC_KEY", "")

	_, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	l := license.NewLicense("hello-shareware", "buyer@example.com", "personal")
	key, err := license.Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	err = verifyAgainstBaked(key)
	if err == nil {
		t.Fatal("verifyAgainstBaked: expected error signing with an unrelated key")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("error = %v, want signature verification failure", err)
	}
}
