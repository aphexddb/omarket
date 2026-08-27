package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aphexddb/omarket/client"
)

// stubOpenBrowser replaces openBrowser with a no-op for the duration of the
// test, so exercising `sell payouts` doesn't actually spawn a browser
// process on the machine running the tests.
func stubOpenBrowser(t *testing.T) {
	t.Helper()
	orig := openBrowser
	openBrowser = func(string) {}
	t.Cleanup(func() { openBrowser = orig })
}

// TestRunSellPayoutsFireAndExit checks `sell payouts` never polls
// sellers/me: it prints the onboarding URL and returns immediately.
// A poll here would be the old 5-minute-spinner behavior this
// replaces.
func TestRunSellPayoutsFireAndExit(t *testing.T) {
	setConfigDir(t, t.TempDir())
	stubOpenBrowser(t)
	if err := client.SaveSellerToken("sk_test"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}

	var payoutHits, sellerMeHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sellers/payouts", func(w http.ResponseWriter, r *http.Request) {
		payoutHits++
		_ = json.NewEncoder(w).Encode(client.PayoutsAccount{
			StripeAccount: "acct_1", OnboardingURL: "https://connect.stripe.com/setup/1",
		})
	})
	mux.HandleFunc("/api/sellers/me", func(w http.ResponseWriter, r *http.Request) {
		sellerMeHits++
		_ = json.NewEncoder(w).Encode(client.SellerMe{SellerID: "sel_1", ChargesEnabled: false})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := runSellPayouts([]string{"-server", srv.URL}); err != nil {
		t.Fatalf("runSellPayouts: %v", err)
	}
	if payoutHits != 1 {
		t.Fatalf("payoutHits = %d, want 1", payoutHits)
	}
	if sellerMeHits != 0 {
		t.Fatalf("sellerMeHits = %d, want 0 (fire-and-exit must not poll)", sellerMeHits)
	}
}

// TestRunSellPayoutsAlreadyEnabledFireAndExit checks the already-enabled
// case also performs no polling.
func TestRunSellPayoutsAlreadyEnabledFireAndExit(t *testing.T) {
	setConfigDir(t, t.TempDir())
	if err := client.SaveSellerToken("sk_test"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}

	var sellerMeHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sellers/payouts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.PayoutsAccount{StripeAccount: "acct_1", OnboardingURL: ""})
	})
	mux.HandleFunc("/api/sellers/me", func(w http.ResponseWriter, r *http.Request) {
		sellerMeHits++
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := runSellPayouts([]string{"-server", srv.URL}); err != nil {
		t.Fatalf("runSellPayouts: %v", err)
	}
	if sellerMeHits != 0 {
		t.Fatalf("sellerMeHits = %d, want 0", sellerMeHits)
	}
}

// TestRunSellStatusWaitStopsWhenEnabled checks `sell status -wait` returns
// as soon as charges_enabled flips true, rather than always running to the
// cap.
func TestRunSellStatusWaitStopsWhenEnabled(t *testing.T) {
	setConfigDir(t, t.TempDir())
	if err := client.SaveSellerToken("sk_test"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}

	orig := sellStatusWaitCap
	sellStatusWaitCap = 5 * time.Second
	t.Cleanup(func() { sellStatusWaitCap = orig })

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(client.SellerMe{SellerID: "sel_1", ChargesEnabled: hits >= 2})
	}))
	defer srv.Close()

	start := time.Now()
	if err := runSellStatus([]string{"-server", srv.URL, "-wait"}); err != nil {
		t.Fatalf("runSellStatus -wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > sellStatusWaitCap {
		t.Fatalf("took %v, want well under the %v cap once charges flipped", elapsed, sellStatusWaitCap)
	}
	if hits < 2 {
		t.Fatalf("hits = %d, want at least 2", hits)
	}
}

// TestRunSellStatusWaitStopsAtCap checks an old server (or one that never
// flips charges_enabled) doesn't wait forever: `sell status -wait` gives up
// at sellStatusWaitCap and still prints whatever status it has.
func TestRunSellStatusWaitStopsAtCap(t *testing.T) {
	setConfigDir(t, t.TempDir())
	if err := client.SaveSellerToken("sk_test"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}

	origCap, origPer := sellStatusWaitCap, sellStatusWaitPerRequest
	sellStatusWaitCap = 150 * time.Millisecond
	sellStatusWaitPerRequest = 10 * time.Millisecond
	t.Cleanup(func() { sellStatusWaitCap, sellStatusWaitPerRequest = origCap, origPer })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An "old server": ignores ?wait= and answers instantly, never enabled.
		_ = json.NewEncoder(w).Encode(client.SellerMe{SellerID: "sel_1", ChargesEnabled: false})
	}))
	defer srv.Close()

	start := time.Now()
	if err := runSellStatus([]string{"-server", srv.URL, "-wait"}); err != nil {
		t.Fatalf("runSellStatus -wait: %v", err)
	}
	elapsed := time.Since(start)
	// Bounded above by the cap plus one cadence floor step (the loop only
	// checks the cap between iterations); bounded below by nothing meaningful
	// against an instant-answering double, so only assert the upper bound.
	if elapsed > sellStatusWaitCap+decayCap+time.Second {
		t.Fatalf("took %v, want it to stop near the %v cap", elapsed, sellStatusWaitCap)
	}
}
