package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aphexddb/omarket/client"
)

func TestGetCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog.json" {
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
	checkoutURL, token, err := c.Buy(context.Background(), "hello-shareware", "buyer@example.com")
	if err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if checkoutURL != "https://checkout.stripe.com/session123" {
		t.Fatalf("checkoutURL = %q", checkoutURL)
	}
	if token != "pt_abc123" {
		t.Fatalf("token = %q", token)
	}

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
