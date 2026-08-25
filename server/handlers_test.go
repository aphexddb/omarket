package server

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"

	"github.com/aphexddb/omarchy-shareware/license"
)

const testWebhookSecret = "whsec_test_secret"

type testEnv struct {
	server *Server
	fake   *fakeStripeClient
	store  *Store
	pub    ed25519.PublicKey
	priv   ed25519.PrivateKey
	apps   []App
}

func newTestEnv(t *testing.T, apps []App) *testEnv {
	t.Helper()

	store, err := OpenStore(filepath.Join(t.TempDir(), "sharewared.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	fake := &fakeStripeClient{}

	appMap := make(map[string]App, len(apps))
	for _, a := range apps {
		appMap[a.ID] = a
	}

	srv := &Server{
		apps:          appMap,
		appList:       apps,
		store:         store,
		stripe:        fake,
		webhookSecret: testWebhookSecret,
		signingKey:    priv,
		baseURL:       "https://market.example.com",
		webDir:        t.TempDir(),
		logger:        log.New(io.Discard, "", 0),
	}

	return &testEnv{server: srv, fake: fake, store: store, pub: pub, priv: priv, apps: apps}
}

func doRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding response %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestHandleCatalog(t *testing.T) {
	apps := []App{{ID: "hello", Name: "Hello", PriceCents: 0}}
	env := newTestEnv(t, apps)

	rec := doRequest(t, env.server, http.MethodGet, "/catalog.json", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got struct {
		Apps []App `json:"apps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got.Apps) != 1 || got.Apps[0].ID != "hello" {
		t.Fatalf("unexpected catalog response: %+v", got)
	}
}

func TestHandleBuy_HappyPath(t *testing.T) {
	apps := []App{{
		ID: "paid", Name: "Paid App", PriceCents: 999, Currency: "usd",
		StripeAccount: "acct_dev123",
	}}
	env := newTestEnv(t, apps)
	env.fake.checkoutURL = "https://checkout.stripe.com/c/pay/cs_test_abc"

	rec := doRequest(t, env.server, http.MethodPost, "/api/buy", map[string]string{
		"app": "paid", "email": "buyer@example.com",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got := decodeJSON[buyResponse](t, rec)
	if got.CheckoutURL != "https://checkout.stripe.com/c/pay/cs_test_abc" {
		t.Fatalf("unexpected checkout url: %s", got.CheckoutURL)
	}
	if !strings.HasPrefix(got.Purchase, "pt_") {
		t.Fatalf("expected purchase token to start with pt_, got %s", got.Purchase)
	}
	if hexPart := strings.TrimPrefix(got.Purchase, "pt_"); len(hexPart) != 32 {
		t.Fatalf("expected 32 hex chars after pt_, got %d in %q", len(hexPart), got.Purchase)
	} else if _, err := hex.DecodeString(hexPart); err != nil {
		t.Fatalf("purchase token suffix not hex: %v", err)
	}

	// 5% floor: 999 * 5 / 100 = 49.
	if env.fake.lastCheckout.FeeCents != 49 {
		t.Fatalf("expected fee 49, got %d", env.fake.lastCheckout.FeeCents)
	}
	if env.fake.lastCheckout.StripeAccount != "acct_dev123" {
		t.Fatalf("expected destination acct_dev123, got %s", env.fake.lastCheckout.StripeAccount)
	}
	if env.fake.lastCheckout.Metadata["purchase"] != got.Purchase {
		t.Fatalf("expected metadata purchase to match token")
	}
	if env.fake.lastCheckout.Metadata["app"] != "paid" {
		t.Fatalf("expected metadata app=paid, got %s", env.fake.lastCheckout.Metadata["app"])
	}

	// Purchase should be stored as pending.
	p, err := env.store.Get(got.Purchase)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if p.Status != StatusPending {
		t.Fatalf("expected pending, got %s", p.Status)
	}
	if p.App != "paid" || p.Email != "buyer@example.com" {
		t.Fatalf("unexpected stored purchase: %+v", p)
	}
}

func TestHandleBuy_UnknownApp(t *testing.T) {
	env := newTestEnv(t, nil)

	rec := doRequest(t, env.server, http.MethodPost, "/api/buy", map[string]string{"app": "nope"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	assertErrorShape(t, rec)
}

func TestHandleBuy_FreeApp(t *testing.T) {
	apps := []App{{ID: "free", Name: "Free App", PriceCents: 0}}
	env := newTestEnv(t, apps)

	rec := doRequest(t, env.server, http.MethodPost, "/api/buy", map[string]string{"app": "free"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertErrorShape(t, rec)
	if env.fake.checkoutCalls != 0 {
		t.Fatalf("expected no checkout session to be created for a free app")
	}
}

func assertErrorShape(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body %q: %v", rec.Body.String(), err)
	}
	if _, ok := body["error"]; !ok {
		t.Fatalf("expected {\"error\":...} shape, got %q", rec.Body.String())
	}
}

func TestHandlePurchase_PendingThenNotFound(t *testing.T) {
	env := newTestEnv(t, nil)

	if err := env.store.Put("pt_abc", Purchase{App: "x", Status: StatusPending, CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rec := doRequest(t, env.server, http.MethodGet, "/api/purchase/pt_abc", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got := decodeJSON[purchaseResponse](t, rec)
	if got.Status != StatusPending || got.LicenseKey != "" {
		t.Fatalf("unexpected response: %+v", got)
	}

	rec = doRequest(t, env.server, http.MethodGet, "/api/purchase/pt_unknown", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown token, got %d", rec.Code)
	}
	assertErrorShape(t, rec)
}

// buildWebhookRequest builds a signed Stripe webhook POST request for a
// checkout.session.completed event carrying the given metadata.
func buildWebhookRequest(t *testing.T, secret, sessionID, purchase, app, email string) *http.Request {
	t.Helper()

	payload := fmt.Sprintf(`{
		"id": "evt_test_1",
		"object": "event",
		"api_version": %q,
		"type": "checkout.session.completed",
		"data": {
			"object": {
				"id": %q,
				"object": "checkout.session",
				"metadata": {"purchase": %q, "app": %q, "email": %q}
			}
		}
	}`, stripe.APIVersion, sessionID, purchase, app, email)

	ts := time.Now()
	sig := webhook.ComputeSignature(ts, []byte(payload), secret)
	header := fmt.Sprintf("t=%d,v1=%s", ts.Unix(), hex.EncodeToString(sig))

	req := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(payload))
	req.Header.Set("Stripe-Signature", header)
	return req
}

func TestHandleWebhook_CompletesPurchase(t *testing.T) {
	env := newTestEnv(t, nil)
	if err := env.store.Put("pt_1", Purchase{App: "hello", Status: StatusPending, CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	req := buildWebhookRequest(t, testWebhookSecret, "cs_1", "pt_1", "hello", "buyer@example.com")
	rec := httptest.NewRecorder()
	env.server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	p, err := env.store.Get("pt_1")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if p.Status != StatusComplete {
		t.Fatalf("expected complete, got %s", p.Status)
	}
	if p.LicenseKey == "" {
		t.Fatal("expected a license key to be stored")
	}

	lic, err := license.Verify(p.LicenseKey, env.pub)
	if err != nil {
		t.Fatalf("license.Verify: %v", err)
	}
	if lic.App != "hello" {
		t.Fatalf("expected license app=hello, got %s", lic.App)
	}
	if lic.EmailSHA256 != license.HashEmail("buyer@example.com") {
		t.Fatalf("expected email hash to match buyer@example.com")
	}

	// GET /api/purchase/{token} should now reflect completion.
	rec2 := doRequest(t, env.server, http.MethodGet, "/api/purchase/pt_1", nil)
	got := decodeJSON[purchaseResponse](t, rec2)
	if got.Status != StatusComplete || got.LicenseKey != p.LicenseKey {
		t.Fatalf("unexpected purchase response: %+v", got)
	}
}

func TestHandleWebhook_Idempotent(t *testing.T) {
	env := newTestEnv(t, nil)
	if err := env.store.Put("pt_2", Purchase{App: "hello", Status: StatusPending, CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	req1 := buildWebhookRequest(t, testWebhookSecret, "cs_2", "pt_2", "hello", "buyer@example.com")
	rec1 := httptest.NewRecorder()
	env.server.Handler().ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first webhook call: expected 200, got %d", rec1.Code)
	}

	p1, err := env.store.Get("pt_2")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}

	// Replay the same event.
	req2 := buildWebhookRequest(t, testWebhookSecret, "cs_2", "pt_2", "hello", "buyer@example.com")
	rec2 := httptest.NewRecorder()
	env.server.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second webhook call: expected 200, got %d", rec2.Code)
	}

	p2, err := env.store.Get("pt_2")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if p2.LicenseKey != p1.LicenseKey {
		t.Fatalf("expected idempotent replay to keep the same license key, got %q then %q", p1.LicenseKey, p2.LicenseKey)
	}
}

func TestHandleWebhook_BadSignature(t *testing.T) {
	env := newTestEnv(t, nil)
	req := buildWebhookRequest(t, "whsec_wrong_secret", "cs_3", "pt_3", "hello", "buyer@example.com")
	rec := httptest.NewRecorder()
	env.server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad signature, got %d", rec.Code)
	}
}

func TestHandleWebhook_UnhandledEventType(t *testing.T) {
	env := newTestEnv(t, nil)

	payload := fmt.Sprintf(`{"id":"evt_x","object":"event","api_version":%q,"type":"account.updated","data":{"object":{}}}`, stripe.APIVersion)
	ts := time.Now()
	sig := webhook.ComputeSignature(ts, []byte(payload), testWebhookSecret)
	header := fmt.Sprintf("t=%d,v1=%s", ts.Unix(), hex.EncodeToString(sig))

	req := httptest.NewRequest(http.MethodPost, "/stripe/webhook", strings.NewReader(payload))
	req.Header.Set("Stripe-Signature", header)
	rec := httptest.NewRecorder()
	env.server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for unhandled event type, got %d", rec.Code)
	}
}

func TestHandleDevOnboard(t *testing.T) {
	env := newTestEnv(t, nil)
	env.fake.expressAccountID = "acct_new1"
	env.fake.accountLinkURL = "https://connect.stripe.com/setup/e/acct_new1"

	rec := doRequest(t, env.server, http.MethodPost, "/api/dev/onboard", map[string]string{"email": "dev@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got := decodeJSON[devOnboardResponse](t, rec)
	if got.Account != "acct_new1" {
		t.Fatalf("expected account acct_new1, got %s", got.Account)
	}
	if got.OnboardingURL != "https://connect.stripe.com/setup/e/acct_new1" {
		t.Fatalf("unexpected onboarding url: %s", got.OnboardingURL)
	}
	if env.fake.lastExpressEmail != "dev@example.com" {
		t.Fatalf("expected express account created with dev@example.com, got %s", env.fake.lastExpressEmail)
	}
	if env.fake.lastAccountLinkID != "acct_new1" {
		t.Fatalf("expected account link created for acct_new1, got %s", env.fake.lastAccountLinkID)
	}
}

func TestHandleHealthz(t *testing.T) {
	env := newTestEnv(t, nil)
	rec := doRequest(t, env.server, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected healthz body: %s", rec.Body.String())
	}
}
