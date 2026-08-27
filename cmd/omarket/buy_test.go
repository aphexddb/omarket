package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aphexddb/omarket/client"
)

// TestRunBuyNoArgBrowsesCatalog covers the "buy-no-arg -> list path" case:
// `omarket buy` with no app id must hit the catalog endpoint, the same one
// `omarket list` uses, rather than the purchase endpoints.
func TestRunBuyNoArgBrowsesCatalog(t *testing.T) {
	var hitCatalog bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/catalog.json":
			hitCatalog = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"apps": []map[string]any{
					{"id": "hello-shareware", "name": "Hello Shareware", "price_cents": 0},
				},
			})
		default:
			t.Errorf("buy with no app arg should only hit /api/catalog.json, got %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runBuy([]string{"-server", srv.URL}); err != nil {
		t.Fatalf("runBuy (no app arg): %v", err)
	}
	if !hitCatalog {
		t.Fatal("runBuy with no app arg never hit /api/catalog.json")
	}
}

// TestRunListIsHiddenAliasForCatalog checks the hidden `omarket list` alias
// still works and hits the same endpoint as `omarket buy` with no args.
func TestRunListIsHiddenAliasForCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/catalog.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apps": []map[string]any{}})
	}))
	defer srv.Close()

	if err := runList([]string{"-server", srv.URL}); err != nil {
		t.Fatalf("runList: %v", err)
	}
}

// TestRunBuyWithArgPurchasesApp covers the "buy-with-arg -> purchase path"
// case: `omarket buy <app>` must run the checkout/poll/save flow, not print
// the catalog.
func TestRunBuyWithArgPurchasesApp(t *testing.T) {
	setConfigDir(t, t.TempDir())

	var hitBuy, hitPurchase bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/buy", func(w http.ResponseWriter, r *http.Request) {
		hitBuy = true
		_ = json.NewEncoder(w).Encode(map[string]string{
			"checkout_url": "https://checkout.stripe.com/session123",
			"purchase":     "pt_test",
		})
	})
	mux.HandleFunc("/api/purchase/pt_test", func(w http.ResponseWriter, r *http.Request) {
		hitPurchase = true
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":      "complete",
			"license_key": "SHRW1.payload.sig",
		})
	})
	mux.HandleFunc("/api/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		t.Error("buy with an app arg should not hit /api/catalog.json")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := runBuy([]string{"-server", srv.URL, "hello-shareware"}); err != nil {
		t.Fatalf("runBuy (with app arg): %v", err)
	}
	if !hitBuy || !hitPurchase {
		t.Fatalf("runBuy with app arg didn't run the purchase flow (hitBuy=%v hitPurchase=%v)", hitBuy, hitPurchase)
	}
	if !client.HasLicense("hello-shareware") {
		t.Fatal("expected the purchased license to be saved to disk")
	}
}
