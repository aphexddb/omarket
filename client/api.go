package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aphexddb/omarket/internal/version"
)

// userAgent identifies this CLI to the server, e.g.
// "omarket/0.1.0 (+https://omarket.dev)".
var userAgent = fmt.Sprintf("omarket/%s (+https://omarket.dev)", version.Version)

// App mirrors a catalog entry as served by GET /catalog.json (SPEC §2/§3).
type App struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Version       string   `json:"version"`
	Homepage      string   `json:"homepage"`
	Source        string   `json:"source"`
	Pkgname       string   `json:"pkgname"`
	PriceCents    int      `json:"price_cents"`
	Currency      string   `json:"currency"`
	StripeAccount string   `json:"stripe_account"`
	Kind          string   `json:"kind"`
	Tags          []string `json:"tags"`
}

// Free reports whether the app has no purchase price.
func (a App) Free() bool { return a.PriceCents == 0 }

type catalogResponse struct {
	Apps []App `json:"apps"`
}

type buyResponse struct {
	CheckoutURL string `json:"checkout_url"`
	Purchase    string `json:"purchase"`
}

type purchaseResponse struct {
	Status     string `json:"status"`
	LicenseKey string `json:"license_key"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// PubKeyEntry describes one signing key as returned by GET /api/pubkey.
type PubKeyEntry struct {
	KeyID       string `json:"key_id"`
	Algorithm   string `json:"algorithm"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

// pubKeyResponse is the GET /api/pubkey response shape. Current servers
// populate Keys; older servers only populate the top-level fields.
type pubKeyResponse struct {
	PublicKey   string        `json:"public_key"`
	KeyID       string        `json:"key_id"`
	Fingerprint string        `json:"fingerprint"`
	Keys        []PubKeyEntry `json:"keys"`
}

// HTTPError is returned by API calls when the server responds with a
// non-2xx status. Callers that need to branch on the status code (e.g. a
// 503 meaning "not configured") can recover it with errors.As.
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: %s (status %d)", e.Method, e.Path, e.Message, e.StatusCode)
	}
	return fmt.Sprintf("%s %s: unexpected status %d", e.Method, e.Path, e.StatusCode)
}

// Client talks to a sharewared server (SPEC §3).
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient builds a Client against baseURL with sane request timeouts.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// GetCatalog fetches the full app catalog.
func (c *Client) GetCatalog(ctx context.Context) ([]App, error) {
	var out catalogResponse
	if err := c.doJSON(ctx, http.MethodGet, "/catalog.json", nil, &out); err != nil {
		return nil, err
	}
	return out.Apps, nil
}

// Buy starts a purchase for app (email optional) and returns the Stripe
// checkout URL and the purchase token used to poll for completion.
func (c *Client) Buy(ctx context.Context, app, email string) (checkoutURL, token string, err error) {
	body := map[string]string{"app": app}
	if email != "" {
		body["email"] = email
	}
	var out buyResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/buy", body, &out); err != nil {
		return "", "", err
	}
	return out.CheckoutURL, out.Purchase, nil
}

// GetPublicKeys fetches the server's Ed25519 license-signing key(s) from
// GET /api/pubkey. Current servers return a "keys" array — every entry from
// it is returned as-is. Older servers return only a single top-level
// public_key (with optional key_id/fingerprint); that shape is normalized
// into a one-entry slice so callers only ever deal with []PubKeyEntry.
func (c *Client) GetPublicKeys(ctx context.Context) ([]PubKeyEntry, error) {
	var out pubKeyResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/pubkey", nil, &out); err != nil {
		return nil, err
	}
	if len(out.Keys) > 0 {
		return out.Keys, nil
	}
	if out.PublicKey == "" {
		return nil, fmt.Errorf("%s/api/pubkey: response has no public_key", c.BaseURL)
	}
	return []PubKeyEntry{{
		KeyID:       out.KeyID,
		PublicKey:   out.PublicKey,
		Fingerprint: out.Fingerprint,
	}}, nil
}

// PollPurchase checks a purchase's status. status is "pending" or
// "complete"; licenseKey is populated once complete.
func (c *Client) PollPurchase(ctx context.Context, token string) (status, licenseKey string, err error) {
	var out purchaseResponse
	path := "/api/purchase/" + url.PathEscape(token)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", "", err
	}
	return out.Status, out.LicenseKey, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONAuth(ctx, method, path, "", body, out)
}

// doJSONAuth is doJSON with an optional bearer token attached as an
// Authorization header (used by the sell API; the buy/catalog API above is
// unauthenticated). An empty bearerToken omits the header.
func (c *Client) doJSONAuth(ctx context.Context, method, path, bearerToken string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var e errorResponse
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return &HTTPError{Method: method, Path: path, StatusCode: resp.StatusCode, Message: e.Error}
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
