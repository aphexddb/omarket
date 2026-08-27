package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// AppPublic mirrors an app as served by the sell API (the pinned
// seller-facing contract). It is a distinct shape from App: App is the
// /api/catalog.json buy-flow view, AppPublic is what sellers see
// when claiming and editing an app.
type AppPublic struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Homepage      string `json:"homepage"`
	PriceUSDCents int    `json:"price_usd_cents"`
	Listed        bool   `json:"listed"`
}

// SellerAccount is the response to POST /api/sellers.
type SellerAccount struct {
	SellerID      string `json:"seller_id"`
	SellerToken   string `json:"seller_token"`
	OnboardingURL string `json:"onboarding_url"`
}

// SellerMe is the response to GET /api/sellers/me.
type SellerMe struct {
	SellerID       string      `json:"seller_id"`
	ChargesEnabled bool        `json:"charges_enabled"`
	OnboardingURL  string      `json:"onboarding_url"`
	Apps           []AppPublic `json:"apps"`
}

// PayoutsAccount is the response to POST /api/sellers/payouts.
type PayoutsAccount struct {
	StripeAccount string `json:"stripe_account"`
	OnboardingURL string `json:"onboarding_url"`
}

type testLicenseResponse struct {
	LicenseKey string `json:"license_key"`
}

// CreateSeller starts seller onboarding: POST /api/sellers with an empty
// JSON body. No auth required; the returned SellerToken authenticates all
// subsequent seller calls.
func (c *Client) CreateSeller(ctx context.Context) (SellerAccount, error) {
	var out SellerAccount
	if err := c.doJSONAuth(ctx, http.MethodPost, "/api/sellers", "", map[string]any{}, &out); err != nil {
		return SellerAccount{}, err
	}
	return out, nil
}

// GetSellerMe fetches the authenticated seller's status: GET /api/sellers/me.
func (c *Client) GetSellerMe(ctx context.Context, sellerToken string) (SellerMe, error) {
	return c.getSellerMe(ctx, c.HTTP, sellerToken, 0)
}

// WaitSellerMe is GetSellerMe with a server-side long-poll hold: it sends
// ?wait=<seconds>, so the server may park the request until the
// seller's charges_enabled flips or wait elapses. It never mints an
// onboarding link on this path — a pending response's OnboardingURL is
// empty even mid-onboarding. Uses the dedicated long-poll client (see
// WaitPurchase) so the default HTTP.Timeout can't cut the hold short.
func (c *Client) WaitSellerMe(ctx context.Context, sellerToken string, wait time.Duration) (SellerMe, error) {
	ctx, cancel := context.WithTimeout(ctx, wait+longPollExtra)
	defer cancel()
	return c.getSellerMe(ctx, c.longPollHTTP, sellerToken, wait)
}

func (c *Client) getSellerMe(ctx context.Context, httpClient *http.Client, sellerToken string, wait time.Duration) (SellerMe, error) {
	path := "/api/sellers/me"
	if wait > 0 {
		path += fmt.Sprintf("?wait=%d", waitSeconds(wait))
	}
	var out SellerMe
	if err := c.doJSONWith(ctx, httpClient, http.MethodGet, path, sellerToken, nil, &out); err != nil {
		return SellerMe{}, err
	}
	return out, nil
}

// StartPayouts starts (or resumes) Stripe Connect onboarding for the
// authenticated seller: POST /api/sellers/payouts with an empty JSON body.
// If the seller already has a Stripe account, the server returns the same
// 200 shape with a fresh onboarding link, or an empty OnboardingURL once
// charges are enabled — callers should treat an empty OnboardingURL as
// "already set up". A 503 means the server has no Stripe configured; use
// errors.As with *HTTPError to detect it.
func (c *Client) StartPayouts(ctx context.Context, sellerToken string) (PayoutsAccount, error) {
	var out PayoutsAccount
	if err := c.doJSONAuth(ctx, http.MethodPost, "/api/sellers/payouts", sellerToken, map[string]any{}, &out); err != nil {
		return PayoutsAccount{}, err
	}
	return out, nil
}

// ClaimApp claims app id for the authenticated seller: POST /api/apps.
func (c *Client) ClaimApp(ctx context.Context, sellerToken, id string) (AppPublic, error) {
	var out AppPublic
	body := map[string]string{"id": id}
	if err := c.doJSONAuth(ctx, http.MethodPost, "/api/apps", sellerToken, body, &out); err != nil {
		return AppPublic{}, err
	}
	return out, nil
}

// PushApp updates a claimed app's listing details: PUT /api/apps/{id}. Only
// the editable fields are sent; the id is not part of the request body.
func (c *Client) PushApp(ctx context.Context, sellerToken string, m Manifest) (AppPublic, error) {
	var out AppPublic
	body := map[string]any{
		"name":            m.Name,
		"description":     m.Description,
		"homepage":        m.Homepage,
		"price_usd_cents": m.PriceUSDCents,
		"ware":            WareOrDefault(m.Ware),
		"comment":         m.Comment,
		"author":          m.Author,
	}
	path := "/api/apps/" + url.PathEscape(m.ID)
	if err := c.doJSONAuth(ctx, http.MethodPut, path, sellerToken, body, &out); err != nil {
		return AppPublic{}, err
	}
	return out, nil
}

// CreateTestLicense mints a test-kind license for a claimed app:
// POST /api/apps/{id}/test-license.
func (c *Client) CreateTestLicense(ctx context.Context, sellerToken, id string) (string, error) {
	var out testLicenseResponse
	path := "/api/apps/" + url.PathEscape(id) + "/test-license"
	if err := c.doJSONAuth(ctx, http.MethodPost, path, sellerToken, nil, &out); err != nil {
		return "", err
	}
	return out.LicenseKey, nil
}
