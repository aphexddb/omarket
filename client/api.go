package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aphexddb/omarket/internal/version"
)

// userAgent identifies this CLI to the server, e.g.
// "omarket/0.1.0 (+https://omarket.dev)".
var userAgent = fmt.Sprintf("omarket/%s (+https://omarket.dev)", version.Version)

// longPollExtra is added to a WaitPurchase/WaitSellerMe request's context
// deadline on top of the requested wait, so the client's own deadline never
// races the server's clamp (SPEC §3.2's 25s cap plus margin).
const longPollExtra = 10 * time.Second

// App mirrors a catalog entry as served by GET /api/catalog.json (SPEC §2/§3).
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
	Ware          string   `json:"ware"`
	Comment       string   `json:"comment"`
	Author        string   `json:"author"`
	Tags          []string `json:"tags"`
}

// Free reports whether the app asks for no money — a ware-only listing,
// where the ware and comment are the whole ask.
//
// Only exactly zero counts. A negative price is not a very good deal, it is
// a broken record, and reporting it as free would show a buyer "FREE" for a
// row the server will refuse to sell them anyway.
func (a App) Free() bool { return a.PriceCents == FreePriceUSDCents }

type catalogResponse struct {
	Apps []App `json:"apps"`
}

// BuyRequest is the POST /api/buy request body. CallbackPort/CallbackNonce
// are optional (SPEC §3.1): when CallbackPort is zero, neither field is
// sent, and the server builds its usual success_url with no loopback
// redirect.
type BuyRequest struct {
	App           string
	Email         string
	CallbackPort  int
	CallbackNonce string
}

// BuyResult is what POST /api/buy hands back, for both kinds of listing.
//
// A priced app yields a CheckoutURL to open and a Purchase token to poll
// while the buyer pays. A ware-only app yields no CheckoutURL, Free set,
// and a Purchase token that is already complete: the server signed the
// license inline, so the first poll returns it. Ware, Comment and Author
// come back on the free path so the CLI can show the person what the app
// asks of them at the moment they acquire it.
//
// Interval/ExpiresIn are the server's cadence hints (SPEC §3.1) — zero when
// the server didn't send them (an old server, or a field it chose not to
// set), in which case callers fall back to their own defaults.
type BuyResult struct {
	CheckoutURL string
	Purchase    string
	Free        bool
	Ware        string
	Comment     string
	Author      string
	Interval    time.Duration
	ExpiresIn   time.Duration
}

type buyResponse struct {
	CheckoutURL string `json:"checkout_url"`
	Purchase    string `json:"purchase"`
	Free        bool   `json:"free"`
	Ware        string `json:"ware"`
	Comment     string `json:"comment"`
	Author      string `json:"author"`
	Interval    int    `json:"interval"`
	ExpiresIn   int    `json:"expires_in"`
}

// Purchase statuses, as reported by GET /api/purchase/{token}.
const (
	PurchasePending  = "pending"
	PurchaseComplete = "complete"
)

// purchaseResponse is the GET /api/purchase/{token} response shape.
// Interval is new (SPEC §3.2): a pending long-poll body may refresh the
// client's cadence mid-wait.
type purchaseResponse struct {
	Status     string `json:"status"`
	LicenseKey string `json:"license_key"`
	Interval   int    `json:"interval"`
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

// maxResponseBytes caps how much of any response body this client will read
// into memory. Every endpoint here answers with a small JSON document; a
// body larger than this is a misconfigured proxy, a wrong URL that landed on
// somebody's homepage, or a server trying to exhaust the client. One
// megabyte is far past anything legitimate and far short of anything
// dangerous.
const maxResponseBytes = 1 << 20

// maxMessageRunes bounds how much of a server's error body is quoted back
// to the user. Enough to carry a real message, short enough that no server
// can turn a failed command into a screenful.
const maxMessageRunes = 200

// HTTPError is returned by API calls when the server responds with a
// non-2xx status. Callers that need to branch on the status code (e.g. a
// 503 meaning "not configured", or a 429 slow_down) can recover it with
// errors.As.
//
// Message is the server's own {"error":"..."} text when it sent one. When it
// sent something else — an HTML error page from a proxy, plain text, an
// empty body — Message holds a sanitized, truncated snippet of that instead,
// because "the CDN returned a 502 page" is a materially different problem
// from "the API rejected your request", and a bare status code can't tell
// them apart.
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string

	// RetryAfter is parsed from a Retry-After response header, when present
	// (seconds form only — SPEC §3.4's slow_down). Zero when absent or
	// unparseable.
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: %s (status %d)", e.Method, e.Path, e.Message, e.StatusCode)
	}
	return fmt.Sprintf("%s %s: unexpected status %d", e.Method, e.Path, e.StatusCode)
}

// Advice returns a next step for this status, or "" for a status with no
// generally useful one. It is deliberately about the status alone: the
// server's own Message already covers what went wrong with *this* request,
// and this covers what the person can do about a whole class of them.
func (e *HTTPError) Advice() string {
	switch e.StatusCode {
	case http.StatusBadRequest:
		return "the server rejected the request; fix the reported field and try again"
	case http.StatusUnauthorized:
		return "your seller token was not accepted — run `omarket sell init` to start a seller account, or check -server points at the right market"
	case http.StatusForbidden:
		return "this account isn't allowed to do that; email aphexddb@gmail.com if you think it should be"
	case http.StatusNotFound:
		return "the server doesn't know about that — check the id, and that -server points at the right market"
	case http.StatusConflict:
		return "that conflicts with something already there; nothing was changed"
	case http.StatusRequestEntityTooLarge:
		return "the request was too large; shorten the long fields and try again"
	case http.StatusTooManyRequests:
		return "you're being rate limited; wait a little and try again"
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "the server is having trouble; nothing was changed — try again in a minute"
	default:
		return ""
	}
}

// Client talks to a sharewared server (SPEC §3).
type Client struct {
	BaseURL string
	HTTP    *http.Client

	// longPollHTTP is a dedicated client (Timeout: 0) used only for
	// WaitPurchase/WaitSellerMe: HTTP's default timeout (15s) would kill a
	// 25s server-side hold before it ever gets a chance to respond. The
	// deadline for these requests comes from the request context instead
	// (wait + longPollExtra), so this client is never unbounded in
	// practice. It shares HTTP's cross-host redirect policy.
	longPollHTTP *http.Client
}

// NewClient builds a Client against baseURL with sane request timeouts.
//
// The redirect policy is the security-relevant part: requests carry a seller
// bearer token, and the default policy would forward it to wherever a 302
// pointed. Redirects within the same host are followed (a server moving
// /x to /x/ is routine); anything crossing to another host is refused, so a
// compromised or misconfigured server cannot turn a redirect into a token
// handoff.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: refuseCrossHostRedirect,
		},
		longPollHTTP: &http.Client{
			Timeout:       0,
			CheckRedirect: refuseCrossHostRedirect,
		},
	}
}

func refuseCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= 5 {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	if req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("refusing to follow a redirect from %s to another host (%s): credentials are not forwarded off-host",
			via[0].URL.Host, req.URL.Host)
	}
	return nil
}

// GetCatalog fetches the full app catalog. The result is never nil, so
// callers can range over it without a check.
func (c *Client) GetCatalog(ctx context.Context) ([]App, error) {
	var out catalogResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/catalog.json", nil, &out); err != nil {
		return nil, err
	}
	if out.Apps == nil {
		return []App{}, nil
	}
	return out.Apps, nil
}

// Buy starts a purchase for req.App (req.Email optional). When req is built
// from a callback listener (req.CallbackPort != 0), the request also
// carries a loopback callback nonce (SPEC §3.1, §5.3) — an old server
// ignores both fields.
//
// The response is checked for internal consistency before it is returned:
// a purchase with no token, or a priced purchase with no checkout URL, is
// reported as an error here rather than becoming a poll loop against a
// token that doesn't exist or an empty link printed to the terminal.
func (c *Client) Buy(ctx context.Context, req BuyRequest) (BuyResult, error) {
	body := map[string]any{"app": req.App}
	if req.Email != "" {
		body["email"] = req.Email
	}
	if req.CallbackPort != 0 {
		body["callback_port"] = req.CallbackPort
		body["callback_nonce"] = req.CallbackNonce
	}
	var out buyResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/buy", body, &out); err != nil {
		return BuyResult{}, err
	}
	if out.Purchase == "" {
		return BuyResult{}, fmt.Errorf("%s/api/buy: server returned no purchase token", c.BaseURL)
	}
	if !out.Free && out.CheckoutURL == "" {
		return BuyResult{}, fmt.Errorf("%s/api/buy: server returned neither a checkout URL nor a free purchase", c.BaseURL)
	}
	res := BuyResult{
		CheckoutURL: out.CheckoutURL,
		Purchase:    out.Purchase,
		Free:        out.Free,
		Ware:        out.Ware,
		Comment:     out.Comment,
		Author:      out.Author,
	}
	if out.Interval > 0 {
		res.Interval = time.Duration(out.Interval) * time.Second
	}
	if out.ExpiresIn > 0 {
		res.ExpiresIn = time.Duration(out.ExpiresIn) * time.Second
	}
	return res, nil
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

// PollPurchase checks a purchase's status once, immediately. status is
// PurchasePending or PurchaseComplete; licenseKey is populated once
// complete.
//
// Both inconsistencies a poll loop can't survive are caught here rather
// than downstream: a status this client doesn't know would otherwise read
// as "not complete yet" and spin until the timeout, and a complete purchase
// with no key would be saved to disk as an empty license file.
func (c *Client) PollPurchase(ctx context.Context, token string) (status, licenseKey string, err error) {
	var out purchaseResponse
	path := "/api/purchase/" + url.PathEscape(token)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", "", err
	}
	return validatePurchaseResponse(out, c.BaseURL, path)
}

// WaitPurchase is PollPurchase with a server-side long-poll hold: it sends
// ?wait=<seconds> (SPEC §3.2), so the server may park the request until the
// purchase completes or wait elapses, whichever comes first. interval is
// the server's refreshed cadence hint, if it sent one (0 otherwise). Uses
// the dedicated long-poll client so the default 15s HTTP.Timeout can't cut
// the hold short; the effective deadline is wait+longPollExtra, applied to
// a derived context.
func (c *Client) WaitPurchase(ctx context.Context, token string, wait time.Duration) (status, licenseKey string, interval time.Duration, err error) {
	ctx, cancel := context.WithTimeout(ctx, wait+longPollExtra)
	defer cancel()

	var out purchaseResponse
	path := fmt.Sprintf("/api/purchase/%s?wait=%d", url.PathEscape(token), waitSeconds(wait))
	if err := c.doJSONWith(ctx, c.longPollHTTP, http.MethodGet, path, "", nil, &out); err != nil {
		return "", "", 0, err
	}
	status, licenseKey, err = validatePurchaseResponse(out, c.BaseURL, path)
	if err != nil {
		return "", "", 0, err
	}
	if out.Interval > 0 {
		interval = time.Duration(out.Interval) * time.Second
	}
	return status, licenseKey, interval, nil
}

// validatePurchaseResponse applies the same status/key consistency checks
// to a decoded purchaseResponse, whether it came from a plain poll or a
// long-poll hold.
func validatePurchaseResponse(out purchaseResponse, baseURL, path string) (status, licenseKey string, err error) {
	switch out.Status {
	case PurchasePending:
		return out.Status, "", nil
	case PurchaseComplete:
		if out.LicenseKey == "" {
			return "", "", fmt.Errorf("%s%s: server reported the purchase complete but sent no license key", baseURL, path)
		}
		return out.Status, out.LicenseKey, nil
	default:
		return "", "", fmt.Errorf("%s%s: server reported an unrecognized purchase status %q", baseURL, path, sanitize(out.Status))
	}
}

// waitSeconds converts a wait duration to the whole-second query value the
// server expects, never negative.
func waitSeconds(wait time.Duration) int {
	s := int(wait / time.Second)
	if s < 0 {
		return 0
	}
	return s
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONWith(ctx, c.HTTP, method, path, "", body, out)
}

// doJSONAuth is doJSON with an optional bearer token attached as an
// Authorization header (used by the sell API; the buy/catalog API above is
// unauthenticated). An empty bearerToken omits the header.
func (c *Client) doJSONAuth(ctx context.Context, method, path, bearerToken string, body, out any) error {
	return c.doJSONWith(ctx, c.HTTP, method, path, bearerToken, body, out)
}

// doJSONWith is doJSONAuth with an explicit *http.Client, so long-poll calls
// can route through the dedicated longPollHTTP client instead of the
// default HTTP one (see WaitPurchase, WaitSellerMe).
//
// Every response body — success or failure — is read through a size cap and
// decoded from bytes rather than streamed into a decoder. Reading it whole
// first is what lets a failure quote what the server actually said: a
// streaming decode over an HTML error page consumes the body, fails, and
// leaves nothing to report but the status.
func (c *Client) doJSONWith(ctx context.Context, httpClient *http.Client, method, path, bearerToken string, body, out any) error {
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
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Unwrap the *url.Error so errors.Is(err, context.Canceled) works
		// for callers distinguishing "the user pressed Ctrl-C" from "the
		// server broke", while still naming the URL that failed.
		return fmt.Errorf("%s %s: %w", method, c.BaseURL+path, err)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode >= 400 {
		herr := &HTTPError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			Message:    errorMessage(raw),
		}
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && secs >= 0 {
				herr.RetryAfter = time.Duration(secs) * time.Second
			}
		}
		return herr
	}
	if readErr != nil {
		return fmt.Errorf("%s %s: reading response: %w", method, c.BaseURL+path, readErr)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: server returned a body this client can't read (status %d): %s",
			method, c.BaseURL+path, resp.StatusCode, sanitize(string(raw)))
	}
	return nil
}

// errorMessage extracts something worth showing from an error response
// body. The API's own {"error":"..."} shape wins. JSON that isn't that
// shape — Cloudflare's problem+json 502 page is the one that bit us —
// is not an API message, so Message stays empty and callers fall back
// to the status. Anything else (HTML, "error code: 502") is quoted as a
// sanitized snippet so a proxy page isn't indistinguishable from silence.
func errorMessage(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	var e errorResponse
	if err := json.Unmarshal(trimmed, &e); err == nil && e.Error != "" {
		return sanitize(e.Error)
	}
	if json.Valid(trimmed) {
		return ""
	}
	return sanitize(string(raw))
}

// sanitize makes an arbitrary server-supplied string safe to print on one
// line of somebody's terminal: control characters (ANSI escapes above all,
// which could otherwise repaint the screen or hide text) collapse to
// spaces, runs of whitespace collapse to one, and the result is truncated.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastWasSpace := false
	count := 0
	for _, r := range s {
		if r == unicode.ReplacementChar || unicode.IsSpace(r) || unicode.IsControl(r) || !unicode.IsPrint(r) {
			if !lastWasSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastWasSpace = true
			}
			continue
		}
		if count >= maxMessageRunes {
			return strings.TrimSpace(b.String()) + "..."
		}
		b.WriteRune(r)
		lastWasSpace = false
		count++
	}
	return strings.TrimSpace(b.String())
}
