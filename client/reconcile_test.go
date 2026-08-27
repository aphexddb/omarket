package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

// reconcileFixture builds a purchase server double and a pending record
// pointing at it.
func reconcileFixture(t *testing.T, token string, handler http.HandlerFunc) (*httptest.Server, client.PendingPurchase) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	now := time.Now()
	return srv, client.PendingPurchase{
		Token:     token,
		App:       "hello-shareware",
		Server:    srv.URL,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(),
	}
}

func TestReconcileCompleteSavesAndDeletes(t *testing.T) {
	setConfigDir(t, t.TempDir())

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	l := license.NewLicense("hello-shareware", "", "personal")
	key, err := license.Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	_, p := reconcileFixture(t, "pt_complete", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "complete", "license_key": key})
	})
	if err := client.SavePending(p); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	results, err := client.Reconcile(context.Background(), pub)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != client.OutcomeSaved {
		t.Fatalf("results = %+v, want one OutcomeSaved", results)
	}
	if !client.HasLicense("hello-shareware") {
		t.Fatal("expected the license to be saved")
	}
	remaining, _ := client.ListPending()
	if len(remaining) != 0 {
		t.Fatalf("expected the pending record to be deleted, got %+v", remaining)
	}
}

func TestReconcilePendingKept(t *testing.T) {
	setConfigDir(t, t.TempDir())
	pub, _, _ := license.GenerateKeypair()

	_, p := reconcileFixture(t, "pt_pending", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	})
	if err := client.SavePending(p); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	results, err := client.Reconcile(context.Background(), pub)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != client.OutcomeKept {
		t.Fatalf("results = %+v, want one OutcomeKept", results)
	}
	remaining, _ := client.ListPending()
	if len(remaining) != 1 {
		t.Fatalf("expected the pending record to survive, got %+v", remaining)
	}
}

func TestReconcile404Dropped(t *testing.T) {
	setConfigDir(t, t.TempDir())
	pub, _, _ := license.GenerateKeypair()

	_, p := reconcileFixture(t, "pt_unknown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown token"})
	})
	if err := client.SavePending(p); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	results, err := client.Reconcile(context.Background(), pub)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != client.OutcomeDropped || results[0].Notice == "" {
		t.Fatalf("results = %+v, want one OutcomeDropped with a notice", results)
	}
	remaining, _ := client.ListPending()
	if len(remaining) != 0 {
		t.Fatalf("expected the pending record to be dropped, got %+v", remaining)
	}
}

func TestReconcileNetworkErrorKept(t *testing.T) {
	setConfigDir(t, t.TempDir())
	pub, _, _ := license.GenerateKeypair()

	now := time.Now()
	p := client.PendingPurchase{
		Token: "pt_offline", App: "hello-shareware", Server: "http://127.0.0.1:1", // nothing listening
		CreatedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}
	if err := client.SavePending(p); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	results, err := client.Reconcile(context.Background(), pub)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != client.OutcomeKept || results[0].Err == nil {
		t.Fatalf("results = %+v, want one OutcomeKept carrying the network error", results)
	}
	if results[0].Notice != "" {
		t.Fatalf("network-error kept results must not carry a user-facing notice, got %q", results[0].Notice)
	}
	remaining, _ := client.ListPending()
	if len(remaining) != 1 {
		t.Fatalf("expected the pending record to survive a network error, got %+v", remaining)
	}
}

func TestReconcileExpiredDropped(t *testing.T) {
	setConfigDir(t, t.TempDir())
	pub, _, _ := license.GenerateKeypair()

	// A server that would fail the test if reconcile ever contacted it: an
	// already-expired (past grace) record must be dropped locally, with no
	// network call at all.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("reconcile must not poll an already-expired record's server")
	}))
	defer srv.Close()

	now := time.Now()
	p := client.PendingPurchase{
		Token: "pt_expired", App: "hello-shareware", Server: srv.URL,
		CreatedAt: now.Add(-48 * time.Hour).Unix(),
		ExpiresAt: now.Add(-25 * time.Hour).Unix(), // well past the 1h grace
	}
	if err := client.SavePending(p); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	results, err := client.Reconcile(context.Background(), pub)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != client.OutcomeDropped || results[0].Notice == "" {
		t.Fatalf("results = %+v, want one OutcomeDropped with a notice", results)
	}
	remaining, _ := client.ListPending()
	if len(remaining) != 0 {
		t.Fatalf("expected the expired record to be dropped, got %+v", remaining)
	}
}

func TestReconcileBoundedToFiveRecords(t *testing.T) {
	setConfigDir(t, t.TempDir())
	pub, _, _ := license.GenerateKeypair()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	}))
	defer srv.Close()

	now := time.Now()
	for i := 0; i < 8; i++ {
		p := client.PendingPurchase{
			Token: "pt_" + string(rune('a'+i)), App: "app", Server: srv.URL,
			CreatedAt: now.Add(time.Duration(-i) * time.Minute).Unix(),
			ExpiresAt: now.Add(time.Hour).Unix(),
		}
		if err := client.SavePending(p); err != nil {
			t.Fatalf("SavePending: %v", err)
		}
	}

	results, err := client.Reconcile(context.Background(), pub)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("len(results) = %d, want 5 (bounded)", len(results))
	}
	if hits != 5 {
		t.Fatalf("server hits = %d, want 5 (bounded)", hits)
	}
}
