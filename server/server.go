// Package server implements the sharewared HTTP API: catalog loading,
// purchase storage, Stripe Checkout/Connect integration, and license
// issuance on payment webhooks. See docs/SPEC.md §2-3.
package server

import (
	"crypto/ed25519"
	"io"
	"log"
	"net/http"
)

// Config configures a new Server.
type Config struct {
	Apps          []App
	Store         *Store
	StripeSecret  string // STRIPE_SECRET_KEY
	WebhookSecret string // STRIPE_WEBHOOK_SECRET
	SigningKey    ed25519.PrivateKey
	BaseURL       string
	WebDir        string
	Logger        *log.Logger
}

// Server holds everything the HTTP handlers need.
type Server struct {
	apps          map[string]App
	appList       []App
	store         *Store
	stripe        stripeClient
	webhookSecret string
	signingKey    ed25519.PrivateKey
	baseURL       string
	webDir        string
	logger        *log.Logger
}

// New builds a Server from cfg, ready to be wrapped in Handler().
func New(cfg Config) *Server {
	apps := make(map[string]App, len(cfg.Apps))
	for _, a := range cfg.Apps {
		apps[a.ID] = a
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	return &Server{
		apps:          apps,
		appList:       cfg.Apps,
		store:         cfg.Store,
		stripe:        newLiveStripeClient(cfg.StripeSecret),
		webhookSecret: cfg.WebhookSecret,
		signingKey:    cfg.SigningKey,
		baseURL:       cfg.BaseURL,
		webDir:        cfg.WebDir,
		logger:        logger,
	}
}

// Handler builds the HTTP handler for the whole API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /catalog.json", s.handleCatalog)
	mux.HandleFunc("POST /api/buy", s.handleBuy)
	mux.HandleFunc("GET /api/purchase/{token}", s.handlePurchase)
	mux.HandleFunc("POST /stripe/webhook", s.handleWebhook)
	mux.HandleFunc("POST /api/dev/onboard", s.handleDevOnboard)

	mux.HandleFunc("GET /{$}", s.staticFile("index.html"))
	mux.HandleFunc("GET /success", s.staticFile("success.html"))
	mux.HandleFunc("GET /cancel", s.staticFile("cancel.html"))
	mux.HandleFunc("GET /dev", s.staticFile("index.html"))

	return mux
}
