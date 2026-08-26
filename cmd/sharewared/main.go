// Command sharewared is the omarchy-shareware server: catalog, purchases,
// Stripe Checkout/Connect, and license issuance. See docs/SPEC.md §3.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aphexddb/omarchy-shareware/internal/version"
	"github.com/aphexddb/omarchy-shareware/license"
	"github.com/aphexddb/omarchy-shareware/server"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(version.String())
			return
		case "-h", "--help", "help":
			fmt.Fprintln(os.Stderr, "usage: sharewared\n       sharewared version")
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sharewared:", err)
		os.Exit(1)
	}
}

func run() error {
	logger := log.New(os.Stdout, "sharewared: ", log.LstdFlags)
	logger.Println(version.String())

	port := envOr("PORT", "8484")
	baseURL := os.Getenv("BASE_URL")
	stripeSecret := os.Getenv("STRIPE_SECRET_KEY")
	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	signingKeyEnv := os.Getenv("PLATFORM_SIGNING_KEY")
	catalogDir := envOr("CATALOG_DIR", "./catalog")
	statePath := envOr("STATE_PATH", "./sharewared.db")
	webDir := envOr("WEB_DIR", "./web")

	if baseURL == "" {
		return errors.New("BASE_URL is required")
	}
	if stripeSecret == "" {
		return errors.New("STRIPE_SECRET_KEY is required")
	}
	if webhookSecret == "" {
		return errors.New("STRIPE_WEBHOOK_SECRET is required")
	}
	if signingKeyEnv == "" {
		return errors.New("PLATFORM_SIGNING_KEY is required")
	}

	signingKey, err := license.DecodePrivateKey(signingKeyEnv)
	if err != nil {
		return fmt.Errorf("decoding PLATFORM_SIGNING_KEY: %w", err)
	}

	store, err := server.OpenStore(statePath)
	if err != nil {
		return fmt.Errorf("opening state store: %w", err)
	}
	defer store.Close()

	apps, err := server.LoadCatalog(catalogDir, logger)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	logger.Printf("loaded %d catalog app(s) from %s", len(apps), catalogDir)

	srv := server.New(server.Config{
		Apps:          apps,
		Store:         store,
		StripeSecret:  stripeSecret,
		WebhookSecret: webhookSecret,
		SigningKey:    signingKey,
		BaseURL:       baseURL,
		WebDir:        webDir,
		Logger:        logger,
	})

	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: srv.Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on :%s", port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		logger.Println("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
