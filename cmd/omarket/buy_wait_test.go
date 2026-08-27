package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aphexddb/omarket/client"
	"github.com/aphexddb/omarket/license"
)

// shrinkBuyTimings overrides the buy wait's timing knobs to millisecond
// scale for the duration of a test, restoring them on cleanup.
func shrinkBuyTimings(t *testing.T, liveBudget, phaseA, longPoll, fastWindow, fastInterval time.Duration) {
	t.Helper()
	origBudget, origPhaseA, origLongPoll, origFastWindow, origFastInterval :=
		buyLiveBudget, buyPhaseADuration, buyLongPollWait, buyFastPollWindow, buyFastPollInterval
	buyLiveBudget, buyPhaseADuration, buyLongPollWait, buyFastPollWindow, buyFastPollInterval =
		liveBudget, phaseA, longPoll, fastWindow, fastInterval
	t.Cleanup(func() {
		buyLiveBudget, buyPhaseADuration, buyLongPollWait, buyFastPollWindow, buyFastPollInterval =
			origBudget, origPhaseA, origLongPoll, origFastWindow, origFastInterval
	})
}

// TestRunBuyBindFailureNoCallbackFields checks that when the loopback
// listener can't be created (simulated here via the newCallback injection
// point), the /api/buy request body carries neither callback_port nor
// callback_nonce — the server must see a request indistinguishable from
// "no callback support" (SPEC §5.3 step 1).
func TestRunBuyBindFailureNoCallbackFields(t *testing.T) {
	setConfigDir(t, t.TempDir())
	shrinkBuyTimings(t, 2*time.Second, 20*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	origNewCallback := newCallback
	newCallback = func() *callbackListener { return nil } // simulate a bind failure
	t.Cleanup(func() { newCallback = origNewCallback })

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	t.Setenv("SHAREWARE_PUBLIC_KEY", license.EncodePublicKey(pub))
	l := license.NewLicense("hello-shareware", "", "personal")
	key, err := license.Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	var capturedBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/buy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"checkout_url": "https://checkout.stripe.com/session123",
			"purchase":     "pt_bind_fail",
		})
	})
	mux.HandleFunc("/api/purchase/pt_bind_fail", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "complete", "license_key": key})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := runBuy([]string{"-server", srv.URL, "hello-shareware"}); err != nil {
		t.Fatalf("runBuy: %v", err)
	}
	if _, ok := capturedBody["callback_port"]; ok {
		t.Errorf("request body carries callback_port with no listener: %+v", capturedBody)
	}
	if _, ok := capturedBody["callback_nonce"]; ok {
		t.Errorf("request body carries callback_nonce with no listener: %+v", capturedBody)
	}
}

// TestRunBuyWakeWhilePendingFastPolls exercises the loopback wake path
// end-to-end: the buy command is started against a server that first
// reports "pending" a few times, we hit /done on its callback listener the
// moment it's up (simulating the success page's redirect beating the
// webhook), and expect the wake to trigger fast-polling rather than waiting
// out a long cadence gap (SPEC §5.3 step 4, "on wake").
func TestRunBuyWakeWhilePendingFastPolls(t *testing.T) {
	setConfigDir(t, t.TempDir())
	shrinkBuyTimings(t, 5*time.Second, 20*time.Millisecond, 50*time.Millisecond, 500*time.Millisecond, 10*time.Millisecond)

	origNewCallback := newCallback
	cbCh := make(chan *callbackListener, 1)
	newCallback = func() *callbackListener {
		cb := origNewCallback()
		cbCh <- cb
		return cb
	}
	t.Cleanup(func() { newCallback = origNewCallback })

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	t.Setenv("SHAREWARE_PUBLIC_KEY", license.EncodePublicKey(pub))
	l := license.NewLicense("hello-shareware", "", "personal")
	key, err := license.Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	var pollCount int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/buy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"checkout_url": "https://checkout.stripe.com/session123",
			"purchase":     "pt_wake",
		})
	})
	mux.HandleFunc("/api/purchase/pt_wake", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&pollCount, 1)
		// Still pending for a few polls after the wake (redirect beats the
		// webhook), then complete — this is exactly the fast-poll window's
		// job.
		if n < 4 {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "complete", "license_key": key})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	done := make(chan error, 1)
	go func() {
		done <- runBuy([]string{"-server", srv.URL, "hello-shareware"})
	}()

	var cb *callbackListener
	select {
	case cb = <-cbCh:
	case <-time.After(2 * time.Second):
		t.Fatal("newCallback was never called")
	}
	if cb == nil {
		t.Fatal("expected a real callback listener (bind should succeed in a test environment)")
	}

	resp, err := getWithRetry(t, httpDoneURL(cb))
	if err != nil {
		t.Fatalf("GET /done: %v", err)
	}
	resp.Body.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runBuy: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runBuy never returned after the callback wake")
	}

	if !client.HasLicense("hello-shareware") {
		t.Fatal("expected the license to be saved")
	}
}

func httpDoneURL(cb *callbackListener) string {
	return "http://127.0.0.1:" + strconv.Itoa(cb.port) + "/done?cb_nonce=" + cb.nonce
}

// TestRunBuyTimeoutThenReconcileLandsKey is the end-to-end guarantee test
// (SPEC §5.4): a buy that never completes within the live budget must leave
// a pending record on disk, and a later reconcile (as `omarket licenses`
// runs on every launch) must land the verified license once the same
// server reports it complete.
func TestRunBuyTimeoutThenReconcileLandsKey(t *testing.T) {
	setConfigDir(t, t.TempDir())
	// A tiny live budget: the wait loop should time out almost immediately
	// (after at most one decay-floor sleep) rather than actually waiting
	// close to 10 minutes.
	shrinkBuyTimings(t, 50*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond, 5*time.Millisecond)

	origNewCallback := newCallback
	newCallback = func() *callbackListener { return nil } // keep this test to the timeout path only
	t.Cleanup(func() { newCallback = origNewCallback })

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	t.Setenv("SHAREWARE_PUBLIC_KEY", license.EncodePublicKey(pub))
	l := license.NewLicense("hello-shareware", "", "personal")
	key, err := license.Sign(l, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	var complete atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/buy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"checkout_url": "https://checkout.stripe.com/session123",
			"purchase":     "pt_reconcile",
		})
	})
	mux.HandleFunc("/api/purchase/pt_reconcile", func(w http.ResponseWriter, r *http.Request) {
		if complete.Load() {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "complete", "license_key": key})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := runBuy([]string{"-server", srv.URL, "hello-shareware"}); err != nil {
		t.Fatalf("runBuy (expected a graceful timeout, not an error): %v", err)
	}
	if client.HasLicense("hello-shareware") {
		t.Fatal("license must not be saved before the purchase completes")
	}
	pending, err := client.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 || pending[0].Token != "pt_reconcile" {
		t.Fatalf("pending = %+v, want one record for pt_reconcile", pending)
	}

	// The purchase completes later (webhook lands after the CLI gave up).
	complete.Store(true)

	results, err := client.Reconcile(t.Context(), pub)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != client.OutcomeSaved {
		t.Fatalf("reconcile results = %+v, want one OutcomeSaved", results)
	}
	if !client.HasLicense("hello-shareware") {
		t.Fatal("expected reconcile to land the verified license")
	}
	remaining, _ := client.ListPending()
	if len(remaining) != 0 {
		t.Fatalf("expected the pending record to be cleared after reconcile, got %+v", remaining)
	}
}
