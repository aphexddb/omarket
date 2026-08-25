package server

import (
	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/account"
	"github.com/stripe/stripe-go/v82/accountlink"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
)

// checkoutParams is everything a checkout session creation needs, kept
// independent of the stripe-go param types so callers/tests don't have to
// build those structs.
type checkoutParams struct {
	AppName       string
	PriceCents    int64
	Currency      string
	StripeAccount string
	FeeCents      int64
	SuccessURL    string
	CancelURL     string
	Email         string
	Metadata      map[string]string
}

// stripeClient is the seam between HTTP handlers and the Stripe API, so
// handlers can be tested without hitting the network.
type stripeClient interface {
	// CreateCheckoutSession creates a Checkout Session and returns its
	// hosted checkout URL.
	CreateCheckoutSession(p checkoutParams) (checkoutURL string, err error)
	// CreateExpressAccount creates a Stripe Connect Express account and
	// returns its id.
	CreateExpressAccount(email string) (accountID string, err error)
	// CreateAccountLink creates an account_onboarding account link for
	// accountID and returns its URL.
	CreateAccountLink(accountID, refreshURL, returnURL string) (url string, err error)
}

// liveStripeClient is the production stripeClient, backed by stripe-go.
type liveStripeClient struct {
	secretKey string
}

func newLiveStripeClient(secretKey string) *liveStripeClient {
	return &liveStripeClient{secretKey: secretKey}
}

func (c *liveStripeClient) sessionClient() checkoutsession.Client {
	return checkoutsession.Client{B: stripe.GetBackend(stripe.APIBackend), Key: c.secretKey}
}

func (c *liveStripeClient) accountClient() account.Client {
	return account.Client{B: stripe.GetBackend(stripe.APIBackend), Key: c.secretKey}
}

func (c *liveStripeClient) accountLinkClient() accountlink.Client {
	return accountlink.Client{B: stripe.GetBackend(stripe.APIBackend), Key: c.secretKey}
}

func (c *liveStripeClient) CreateCheckoutSession(p checkoutParams) (string, error) {
	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(p.SuccessURL),
		CancelURL:  stripe.String(p.CancelURL),
		Metadata:   p.Metadata,
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(p.Currency),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(p.AppName),
					},
					UnitAmount: stripe.Int64(p.PriceCents),
				},
			},
		},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			ApplicationFeeAmount: stripe.Int64(p.FeeCents),
			TransferData: &stripe.CheckoutSessionPaymentIntentDataTransferDataParams{
				Destination: stripe.String(p.StripeAccount),
			},
		},
	}
	if p.Email != "" {
		params.CustomerEmail = stripe.String(p.Email)
	}

	sess, err := c.sessionClient().New(params)
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}

func (c *liveStripeClient) CreateExpressAccount(email string) (string, error) {
	params := &stripe.AccountParams{
		Type: stripe.String(string(stripe.AccountTypeExpress)),
	}
	if email != "" {
		params.Email = stripe.String(email)
	}

	acct, err := c.accountClient().New(params)
	if err != nil {
		return "", err
	}
	return acct.ID, nil
}

func (c *liveStripeClient) CreateAccountLink(accountID, refreshURL, returnURL string) (string, error) {
	params := &stripe.AccountLinkParams{
		Account:    stripe.String(accountID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String(string(stripe.AccountLinkTypeAccountOnboarding)),
	}

	link, err := c.accountLinkClient().New(params)
	if err != nil {
		return "", err
	}
	return link.URL, nil
}
