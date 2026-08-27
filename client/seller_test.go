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

func TestCreateSeller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sellers" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(client.SellerAccount{
			SellerID:      "sel_123",
			SellerToken:   "sk_test_abc123",
			OnboardingURL: "https://connect.stripe.com/setup/123",
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	acct, err := c.CreateSeller(context.Background())
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	if acct.SellerID != "sel_123" || acct.SellerToken != "sk_test_abc123" {
		t.Fatalf("acct = %+v", acct)
	}
}

func TestGetSellerMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sellers/me" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_abc123" {
			t.Errorf("Authorization header = %q", got)
		}
		_ = json.NewEncoder(w).Encode(client.SellerMe{
			SellerID:       "sel_123",
			ChargesEnabled: true,
			Apps: []client.AppPublic{
				{ID: "hello-shareware", Listed: true},
			},
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	me, err := c.GetSellerMe(context.Background(), "sk_test_abc123")
	if err != nil {
		t.Fatalf("GetSellerMe: %v", err)
	}
	if !me.ChargesEnabled || len(me.Apps) != 1 {
		t.Fatalf("me = %+v", me)
	}
}

// TestWaitSellerMeSendsWaitParam checks WaitSellerMe sends ?wait=N and
// parses the response normally.
func TestWaitSellerMeSendsWaitParam(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(client.SellerMe{SellerID: "sel_1", ChargesEnabled: true})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	me, err := c.WaitSellerMe(context.Background(), "sk_test", 7*time.Second)
	if err != nil {
		t.Fatalf("WaitSellerMe: %v", err)
	}
	if gotQuery != "wait=7" {
		t.Fatalf("query = %q, want wait=7", gotQuery)
	}
	if !me.ChargesEnabled {
		t.Fatalf("me = %+v", me)
	}
}

// TestWaitSellerMeUsesDedicatedClient mirrors the WaitPurchase regression
// test: a held response must survive a short synthetic HTTP.Timeout because
// WaitSellerMe routes through the dedicated long-poll client.
func TestWaitSellerMeUsesDedicatedClient(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_ = json.NewEncoder(w).Encode(client.SellerMe{SellerID: "sel_1", ChargesEnabled: true})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	c.HTTP.Timeout = 30 * time.Millisecond

	go func() {
		time.Sleep(150 * time.Millisecond)
		close(release)
	}()

	me, err := c.WaitSellerMe(context.Background(), "sk_test", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitSellerMe: %v", err)
	}
	if !me.ChargesEnabled {
		t.Fatalf("me = %+v", me)
	}
}

func TestStartPayoutsFreshURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sellers/payouts" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_abc123" {
			t.Errorf("Authorization header = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(client.PayoutsAccount{
			StripeAccount: "acct_123",
			OnboardingURL: "https://connect.stripe.com/setup/123",
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	acct, err := c.StartPayouts(context.Background(), "sk_test_abc123")
	if err != nil {
		t.Fatalf("StartPayouts: %v", err)
	}
	if acct.StripeAccount != "acct_123" {
		t.Fatalf("acct.StripeAccount = %q", acct.StripeAccount)
	}
	if acct.OnboardingURL != "https://connect.stripe.com/setup/123" {
		t.Fatalf("acct.OnboardingURL = %q", acct.OnboardingURL)
	}
}

func TestStartPayoutsAlreadyEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.PayoutsAccount{
			StripeAccount: "acct_123",
			OnboardingURL: "",
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	acct, err := c.StartPayouts(context.Background(), "sk_test_abc123")
	if err != nil {
		t.Fatalf("StartPayouts: %v", err)
	}
	if acct.StripeAccount != "acct_123" {
		t.Fatalf("acct.StripeAccount = %q", acct.StripeAccount)
	}
	if acct.OnboardingURL != "" {
		t.Fatalf("acct.OnboardingURL = %q, want empty (already enabled)", acct.OnboardingURL)
	}
}

func TestStartPayoutsNotConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "stripe not configured"})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	_, err := c.StartPayouts(context.Background(), "sk_test_abc123")
	if err == nil {
		t.Fatal("expected error for 503")
	}
	var herr *client.HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("err = %v, want *client.HTTPError", err)
	}
	if herr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("herr.StatusCode = %d, want 503", herr.StatusCode)
	}
	if herr.Message != "stripe not configured" {
		t.Fatalf("herr.Message = %q", herr.Message)
	}
}

func TestClaimApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/apps" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_abc123" {
			t.Errorf("Authorization header = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["id"] != "hello-shareware" {
			t.Errorf("body id = %q, want hello-shareware", body["id"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(client.AppPublic{
			ID:            "hello-shareware",
			Name:          "hello-shareware",
			PriceUSDCents: 0,
			Listed:        false,
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	app, err := c.ClaimApp(context.Background(), "sk_test_abc123", "hello-shareware")
	if err != nil {
		t.Fatalf("ClaimApp: %v", err)
	}
	if app.ID != "hello-shareware" {
		t.Fatalf("app.ID = %q", app.ID)
	}
	if app.Listed {
		t.Fatal("freshly claimed app should not be listed")
	}
}

func TestClaimAppConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "app id taken"})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	if _, err := c.ClaimApp(context.Background(), "sk_test_abc123", "taken-app"); err == nil {
		t.Fatal("expected error for 409 conflict")
	}
}

func TestPushApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/apps/hello-shareware" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if _, hasID := body["id"]; hasID {
			t.Errorf("PUT body should not include id: %+v", body)
		}
		name, _ := body["name"].(string)
		_ = json.NewEncoder(w).Encode(client.AppPublic{
			ID:            "hello-shareware",
			Name:          name,
			PriceUSDCents: 900,
		})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	app, err := c.PushApp(context.Background(), "sk_test_abc123", client.Manifest{
		ID:            "hello-shareware",
		Name:          "Hello Shareware",
		Description:   "desc",
		Homepage:      "https://hello.example.com",
		PriceUSDCents: 900,
	})
	if err != nil {
		t.Fatalf("PushApp: %v", err)
	}
	if app.Name != "Hello Shareware" {
		t.Fatalf("app.Name = %q", app.Name)
	}
}

func TestCreateTestLicense(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/apps/hello-shareware/test-license" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_abc123" {
			t.Errorf("Authorization header = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"license_key": "SHRW1.payload.sig"})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	key, err := c.CreateTestLicense(context.Background(), "sk_test_abc123", "hello-shareware")
	if err != nil {
		t.Fatalf("CreateTestLicense: %v", err)
	}
	if key != "SHRW1.payload.sig" {
		t.Fatalf("key = %q", key)
	}
}
