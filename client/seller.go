package client

import (
	"context"
	"net/http"
	"net/url"
)

// AppPublic mirrors an app as served by the sell/admin API (the pinned
// seller-facing contract). It is a distinct shape from App: App is the
// catalog.json/buy-flow view (SPEC §2/§3), AppPublic is what sellers and
// admins see when claiming, editing, and curating an app.
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
	var out SellerMe
	if err := c.doJSONAuth(ctx, http.MethodGet, "/api/sellers/me", sellerToken, nil, &out); err != nil {
		return SellerMe{}, err
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
// the editable fields (name, description, homepage, price) are sent; the id
// is not part of the request body.
func (c *Client) PushApp(ctx context.Context, sellerToken string, m Manifest) (AppPublic, error) {
	var out AppPublic
	body := map[string]any{
		"name":            m.Name,
		"description":     m.Description,
		"homepage":        m.Homepage,
		"price_usd_cents": m.PriceUSDCents,
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

// AdminSetListed curates whether app id appears in the browse catalog:
// POST /api/admin/apps/{id}/listed. adminToken is the platform operator's
// bearer token (OMARKET_ADMIN_TOKEN), not a seller token.
func (c *Client) AdminSetListed(ctx context.Context, adminToken, id string, listed bool) (AppPublic, error) {
	var out AppPublic
	path := "/api/admin/apps/" + url.PathEscape(id) + "/listed"
	body := map[string]bool{"listed": listed}
	if err := c.doJSONAuth(ctx, http.MethodPost, path, adminToken, body, &out); err != nil {
		return AppPublic{}, err
	}
	return out, nil
}
