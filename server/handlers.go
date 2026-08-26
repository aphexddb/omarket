package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"

	"github.com/aphexddb/omarchy-shareware/license"
)

// writeJSON encodes v as the JSON response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the {"error":"..."} shape used by every endpoint.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	apps := s.appList
	if apps == nil {
		apps = []App{}
	}
	writeJSON(w, http.StatusOK, map[string][]App{"apps": apps})
}

type buyRequest struct {
	App   string `json:"app"`
	Email string `json:"email"`
}

type buyResponse struct {
	CheckoutURL string `json:"checkout_url"`
	Purchase    string `json:"purchase"`
}

func (s *Server) handleBuy(w http.ResponseWriter, r *http.Request) {
	var req buyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	app, ok := s.apps[req.App]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown app")
		return
	}
	if app.PriceCents <= 0 {
		writeError(w, http.StatusBadRequest, "app is free")
		return
	}

	token, err := newPurchaseToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate purchase token")
		return
	}

	feeCents := app.PriceCents * 5 / 100

	checkoutURL, err := s.stripe.CreateCheckoutSession(checkoutParams{
		AppName:       app.Name,
		PriceCents:    app.PriceCents,
		Currency:      app.Currency,
		StripeAccount: app.StripeAccount,
		FeeCents:      feeCents,
		SuccessURL:    s.baseURL + "/success?purchase=" + token,
		CancelURL:     s.baseURL + "/cancel",
		Email:         req.Email,
		Metadata: map[string]string{
			"app":      app.ID,
			"purchase": token,
			"email":    req.Email,
		},
	})
	if err != nil {
		s.logger.Printf("buy: creating checkout session for %s: %v", app.ID, err)
		writeError(w, http.StatusBadGateway, "failed to create checkout session")
		return
	}

	err = s.store.Put(token, Purchase{
		App:       app.ID,
		Email:     req.Email,
		Status:    StatusPending,
		CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.logger.Printf("buy: storing purchase %s: %v", token, err)
		writeError(w, http.StatusInternalServerError, "failed to store purchase")
		return
	}

	writeJSON(w, http.StatusOK, buyResponse{CheckoutURL: checkoutURL, Purchase: token})
}

func newPurchaseToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pt_" + hex.EncodeToString(b), nil
}

type purchaseResponse struct {
	Status     string `json:"status"`
	LicenseKey string `json:"license_key,omitempty"`
}

func (s *Server) handlePurchase(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	p, err := s.store.Get(token)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "unknown purchase")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up purchase")
		return
	}

	writeJSON(w, http.StatusOK, purchaseResponse{Status: p.Status, LicenseKey: p.LicenseKey})
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	const maxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// IgnoreAPIVersionMismatch: accounts pinned to older API versions render
	// events with that version; we only read stable fields (id, metadata).
	event, err := webhook.ConstructEventWithOptions(payload, r.Header.Get("Stripe-Signature"), s.webhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		s.logger.Printf("webhook: rejecting event: %v", err)
		writeError(w, http.StatusBadRequest, "webhook signature verification failed")
		return
	}

	if event.Type != stripe.EventTypeCheckoutSessionCompleted {
		w.WriteHeader(http.StatusOK)
		return
	}

	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		s.logger.Printf("webhook: decoding checkout session: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	token := sess.Metadata["purchase"]
	appID := sess.Metadata["app"]
	email := sess.Metadata["email"]

	if token == "" || appID == "" {
		s.logger.Printf("webhook: checkout session %s missing purchase/app metadata", sess.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	p, err := s.store.Get(token)
	if err != nil {
		s.logger.Printf("webhook: unknown purchase token %s: %v", token, err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if p.Status == StatusComplete {
		// Idempotent: already handled.
		w.WriteHeader(http.StatusOK)
		return
	}

	lic := license.NewLicense(appID, email, "personal")
	key, err := license.Sign(lic, s.signingKey)
	if err != nil {
		s.logger.Printf("webhook: signing license for %s: %v", token, err)
		writeError(w, http.StatusInternalServerError, "failed to sign license")
		return
	}

	p.Status = StatusComplete
	p.LicenseKey = key
	if err := s.store.Put(token, p); err != nil {
		s.logger.Printf("webhook: storing completed purchase %s: %v", token, err)
		writeError(w, http.StatusInternalServerError, "failed to store purchase")
		return
	}

	w.WriteHeader(http.StatusOK)
}

type devOnboardRequest struct {
	Email string `json:"email"`
}

type devOnboardResponse struct {
	Account       string `json:"account"`
	OnboardingURL string `json:"onboarding_url"`
}

func (s *Server) handleDevOnboard(w http.ResponseWriter, r *http.Request) {
	var req devOnboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	accountID, err := s.stripe.CreateExpressAccount(req.Email)
	if err != nil {
		s.logger.Printf("dev/onboard: creating express account: %v", err)
		writeError(w, http.StatusBadGateway, "failed to create Stripe account")
		return
	}

	devURL := s.baseURL + "/dev"
	onboardingURL, err := s.stripe.CreateAccountLink(accountID, devURL, devURL)
	if err != nil {
		s.logger.Printf("dev/onboard: creating account link: %v", err)
		writeError(w, http.StatusBadGateway, "failed to create onboarding link")
		return
	}

	writeJSON(w, http.StatusOK, devOnboardResponse{Account: accountID, OnboardingURL: onboardingURL})
}

func (s *Server) staticFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(s.webDir, name))
	}
}
