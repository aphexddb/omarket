package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
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
