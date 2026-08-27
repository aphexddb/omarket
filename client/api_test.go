package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aphexddb/omarket/client"
)

func TestGetCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/catalog.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"apps": []map[string]any{
				{"id": "hello-shareware", "name": "Hello Shareware", "price_cents": 0},
				{"id": "paid-app", "name": "Paid App", "price_cents": 900},
			},
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	apps, err := c.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("len(apps) = %d, want 2", len(apps))
	}
	if !apps[0].Free() {
		t.Fatalf("apps[0] should be free: %+v", apps[0])
	}
	if apps[1].Free() {
		t.Fatalf("apps[1] should not be free: %+v", apps[1])
	}
}

func TestBuyAndPollPurchaseToCompletion(t *testing.T) {
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/buy", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode buy body: %v", err)
		}
		if body["app"] != "hello-shareware" || body["email"] != "buyer@example.com" {
			t.Errorf("unexpected buy body: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"checkout_url": "https://checkout.stripe.com/session123",
			"purchase":     "pt_abc123",
		})
	})
	mux.HandleFunc("/api/purchase/pt_abc123", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls < 3 {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":      "complete",
			"license_key": "SHRW1.payload.sig",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.NewClient(srv.URL)
	res, err := c.Buy(context.Background(), client.BuyRequest{App: "hello-shareware", Email: "buyer@example.com"})
	if err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if res.CheckoutURL != "https://checkout.stripe.com/session123" {
		t.Fatalf("checkoutURL = %q", res.CheckoutURL)
	}
	if res.Purchase != "pt_abc123" {
		t.Fatalf("token = %q", res.Purchase)
	}
	token := res.Purchase

	var status, key string
	for i := 0; i < 5; i++ {
		status, key, err = c.PollPurchase(context.Background(), token)
		if err != nil {
			t.Fatalf("PollPurchase: %v", err)
		}
		if status == "complete" {
			break
		}
	}
	if status != "complete" {
		t.Fatalf("purchase never completed, last status %q", status)
	}
	if key != "SHRW1.payload.sig" {
		t.Fatalf("license key = %q", key)
	}
}

func TestGetPublicKeysCurrentShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pubkey" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"public_key":  "primaryKeyBase64==",
			"key_id":      "pk_primary000000",
			"fingerprint": "SHA256:primary",
			"keys": []map[string]any{
				{"key_id": "pk_primary000000", "algorithm": "ed25519", "public_key": "primaryKeyBase64==", "fingerprint": "SHA256:primary"},
				{"key_id": "pk_secondary00000", "algorithm": "ed25519", "public_key": "secondaryKeyBase64==", "fingerprint": "SHA256:secondary"},
			},
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	keys, err := c.GetPublicKeys(context.Background())
	if err != nil {
		t.Fatalf("GetPublicKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
	if keys[0].KeyID != "pk_primary000000" || keys[1].KeyID != "pk_secondary00000" {
		t.Fatalf("unexpected keys: %+v", keys)
	}
}

func TestGetPublicKeysLegacyShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pubkey" {
			http.NotFound(w, r)
			return
		}
		// Older server: only the top-level public_key, no "keys" array.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"public_key": "legacyKeyBase64==",
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	keys, err := c.GetPublicKeys(context.Background())
	if err != nil {
		t.Fatalf("GetPublicKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].PublicKey != "legacyKeyBase64==" {
		t.Fatalf("keys[0].PublicKey = %q", keys[0].PublicKey)
	}
	if keys[0].KeyID != "" {
		t.Fatalf("keys[0].KeyID = %q, want empty (legacy server didn't send one)", keys[0].KeyID)
	}
}

func TestGetPublicKeysEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	if _, err := c.GetPublicKeys(context.Background()); err == nil {
		t.Fatal("GetPublicKeys: expected error for response with no keys")
	}
}

// TestWaitPurchaseUsesDedicatedClient is a regression test for G7: the
// default *http.Client (15s Timeout, formerly the *only* client) would kill
// a long-poll hold well before the server's 25s cap. WaitPurchase must
// route through the dedicated long-poll client instead, so a hold longer
// than a short, synthetic c.HTTP.Timeout still succeeds.
func TestWaitPurchaseUsesDedicatedClient(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/purchase/pt_hold", func(w http.ResponseWriter, r *http.Request) {
		<-release // hold the response well past c.HTTP's short timeout below
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "complete", "license_key": "SHRW1.a.b"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.NewClient(srv.URL)
	c.HTTP.Timeout = 30 * time.Millisecond // would abort a held request almost immediately

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(150 * time.Millisecond)
		close(release)
	}()

	status, key, _, err := c.WaitPurchase(context.Background(), "pt_hold", 5*time.Second)
	<-done
	if err != nil {
		t.Fatalf("WaitPurchase: %v (the dedicated long-poll client should not inherit HTTP's short timeout)", err)
	}
	if status != "complete" || key != "SHRW1.a.b" {
		t.Fatalf("status=%q key=%q, want complete/SHRW1.a.b", status, key)
	}
}

// TestWaitPurchaseInterval checks a pending long-poll body's optional
// interval field is surfaced to the caller (the mid-wait cadence
// refresh).
func TestWaitPurchaseInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending", "interval": 7})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	status, _, interval, err := c.WaitPurchase(context.Background(), "pt_x", time.Second)
	if err != nil {
		t.Fatalf("WaitPurchase: %v", err)
	}
	if status != "pending" {
		t.Fatalf("status = %q, want pending", status)
	}
	if interval != 7*time.Second {
		t.Fatalf("interval = %v, want 7s", interval)
	}
}

// TestHTTPErrorRetryAfterParsed checks the Retry-After response header
// (seconds form) lands on HTTPError.RetryAfter.
func TestHTTPErrorRetryAfterParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	_, _, err := c.PollPurchase(context.Background(), "pt_limited")
	if err == nil {
		t.Fatal("expected an error for a 429 response")
	}
	var herr *client.HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("expected *client.HTTPError, got %T: %v", err, err)
	}
	if herr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want 429", herr.StatusCode)
	}
	if herr.RetryAfter != 3*time.Second {
		t.Fatalf("RetryAfter = %v, want 3s", herr.RetryAfter)
	}
}

// TestHTTPErrorNoRetryAfter checks a 4xx with no Retry-After header leaves
// RetryAfter at its zero value rather than panicking or guessing.
func TestHTTPErrorNoRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown token"})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	_, _, err := c.PollPurchase(context.Background(), "pt_missing")
	var herr *client.HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("expected *client.HTTPError, got %T: %v", err, err)
	}
	if herr.RetryAfter != 0 {
		t.Fatalf("RetryAfter = %v, want 0", herr.RetryAfter)
	}
}

func TestPollPurchaseUnknownToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown token"})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	if _, _, err := c.PollPurchase(context.Background(), "nope"); err == nil {
		t.Fatal("expected error for unknown token")
	}
}
