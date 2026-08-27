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

func TestRunSellHelp(t *testing.T) {
	setConfigDir(t, t.TempDir())

	for _, args := range [][]string{nil, {"-h"}, {"--help"}, {"help"}} {
		t.Run(fmtArgs(args), func(t *testing.T) {
			var err error
			out := captureStderr(t, func() {
				err = runSell(args)
			})
			if err != nil {
				t.Fatalf("runSell(%v): %v", args, err)
			}
			if !strings.Contains(out, "usage: omarket sell") {
				t.Fatalf("stderr missing sell usage:\n%s", out)
			}
			if !strings.Contains(out, "omarket") || !strings.Contains(out, "reserved") {
				t.Fatalf("stderr missing reserved-name note:\n%s", out)
			}
		})
	}
}

func TestRunSellUnknownCommandPrintsUsage(t *testing.T) {
	setConfigDir(t, t.TempDir())

	var err error
	out := captureStderr(t, func() {
		err = runSell([]string{"bogus"})
	})
	if err == nil {
		t.Fatal("runSell([bogus]): expected error")
	}
	if strings.Contains(err.Error(), "usage:") {
		t.Fatalf("error should not be a usage string: %v", err)
	}
	if !strings.Contains(err.Error(), `"bogus"`) {
		t.Fatalf("error missing unknown command: %v", err)
	}
	if !strings.Contains(out, "usage: omarket sell") {
		t.Fatalf("stderr missing sell usage:\n%s", out)
	}
}

func TestRunSellBareAliasesStatus(t *testing.T) {
	setConfigDir(t, t.TempDir())
	if err := client.SaveSellerToken("st_bare"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sellers/me" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(client.SellerMe{SellerID: "sel_bare"})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OMARKET_SERVER", srv.URL)

	var err error
	out := captureStdout(t, func() {
		err = runSell(nil)
	})
	if err != nil {
		t.Fatalf("runSell(nil) with token: %v", err)
	}
	if !strings.Contains(out, "sel_bare") {
		t.Fatalf("stdout missing seller status:\n%s", out)
	}
}

func TestRunSellClaimMissingID(t *testing.T) {
	var err error
	out := captureStderr(t, func() {
		err = runSellClaim(nil)
	})
	if err == nil {
		t.Fatal("runSellClaim(nil): expected error")
	}
	if strings.HasPrefix(err.Error(), "usage:") {
		t.Fatalf("error should not start with usage: %v", err)
	}
	if !strings.Contains(out, "usage: omarket sell claim") {
		t.Fatalf("stderr missing claim usage:\n%s", out)
	}
	if !strings.Contains(out, "reserved") {
		t.Fatalf("stderr missing reserved-name note:\n%s", out)
	}
}

func TestRunSellClaimHelp(t *testing.T) {
	var err error
	out := captureStderr(t, func() {
		err = runSellClaim([]string{"-h"})
	})
	if err != nil {
		t.Fatalf("runSellClaim(-h): %v", err)
	}
	if !strings.Contains(out, "usage: omarket sell claim") {
		t.Fatalf("stderr missing claim usage:\n%s", out)
	}
	if !strings.Contains(out, "<app-id>") {
		t.Fatalf("stderr missing required app-id:\n%s", out)
	}
}

func TestClaimError(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		err     error
		want    string
		notWant []string
	}{
		{
			name: "reserved 409",
			id:   "omarket",
			err: &client.HTTPError{
				Method: "POST", Path: "/api/apps", StatusCode: http.StatusConflict,
				Message: "app id is reserved",
			},
			want:    `"omarket" is reserved by the platform`,
			notWant: []string{"POST ", "(status ", "/api/apps"},
		},
		{
			name: "taken 409",
			id:   "hello-shareware",
			err: &client.HTTPError{
				Method: "POST", Path: "/api/apps", StatusCode: http.StatusConflict,
				Message: "app id taken",
			},
			want:    `"hello-shareware" is already claimed`,
			notWant: []string{"POST ", "(status "},
		},
		{
			name: "401 uses advice",
			id:   "hello-shareware",
			err: &client.HTTPError{
				Method: "POST", Path: "/api/apps", StatusCode: http.StatusUnauthorized,
				Message: "unauthorized",
			},
			want:    "sell init",
			notWant: []string{"POST ", "(status "},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claimError(tc.id, tc.err).Error()
			if !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want substring %q", got, tc.want)
			}
			for _, n := range tc.notWant {
				if strings.Contains(got, n) {
					t.Fatalf("got %q, leaked %q", got, n)
				}
			}
		})
	}
}

func TestRunSellClaimReserved(t *testing.T) {
	setConfigDir(t, t.TempDir())
	if err := client.SaveSellerToken("st_claim"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "app id is reserved"})
	}))
	t.Cleanup(srv.Close)

	err := runSellClaim([]string{"-server", srv.URL, "omarket"})
	if err == nil {
		t.Fatal("expected reserved-id error")
	}
	got := err.Error()
	if !strings.Contains(got, "reserved") {
		t.Fatalf("got %q, want reserved", got)
	}
	for _, n := range []string{"POST ", "(status ", "/api/apps"} {
		if strings.Contains(got, n) {
			t.Fatalf("got %q, leaked %q", got, n)
		}
	}
}

func TestMissingManifestErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setConfigDir(t, dir)

	t.Run("push", func(t *testing.T) {
		err := runSellPush(nil)
		if err == nil {
			t.Fatal("expected missing-manifest error")
		}
		got := err.Error()
		if !strings.Contains(got, "omarket sell claim") {
			t.Fatalf("got %q, want a claim hint", got)
		}
		if strings.Contains(got, "no such file") || strings.Contains(got, "open ") {
			t.Fatalf("got %q, leaked OS error", got)
		}
	})
	t.Run("testkey", func(t *testing.T) {
		err := runSellTestkey(nil)
		if err == nil {
			t.Fatal("expected missing-manifest error")
		}
		got := err.Error()
		if !strings.Contains(got, "omarket sell claim") {
			t.Fatalf("got %q, want a claim hint", got)
		}
		if !strings.Contains(got, "omarket sell testkey") {
			t.Fatalf("got %q, want a testkey <app-id> hint", got)
		}
		if strings.Contains(got, "no such file") || strings.Contains(got, "open ") {
			t.Fatalf("got %q, leaked OS error", got)
		}
	})
}

func TestSellAPIError(t *testing.T) {
	err := sellAPIError("fetching seller status", &client.HTTPError{
		Method: "GET", Path: "/api/sellers/me", StatusCode: http.StatusUnauthorized,
		Message: "unauthorized",
	})
	got := err.Error()
	if !strings.Contains(got, "unauthorized") {
		t.Fatalf("got %q, want server message", got)
	}
	if !strings.Contains(got, "sell init") {
		t.Fatalf("got %q, want Advice()", got)
	}
	if strings.Contains(got, "GET ") || strings.Contains(got, "(status ") {
		t.Fatalf("got %q, leaked HTTP internals", got)
	}
}

func fmtArgs(args []string) string {
	if len(args) == 0 {
		return "no-args"
	}
	return strings.Join(args, " ")
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

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stderr: %v", err)
	}
	_ = r.Close()
	return buf.String()
}
