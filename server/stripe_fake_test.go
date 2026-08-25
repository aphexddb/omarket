package server

// fakeStripeClient is a stripeClient test double that never touches the
// network, so handler tests can run offline and assert on exactly what
// would have been sent to Stripe.
type fakeStripeClient struct {
	checkoutURL   string
	checkoutErr   error
	lastCheckout  checkoutParams
	checkoutCalls int

	expressAccountID string
	expressErr       error
	lastExpressEmail string

	accountLinkURL         string
	accountLinkErr         error
	lastAccountLinkID      string
	lastAccountLinkRefresh string
	lastAccountLinkReturn  string
}

func (f *fakeStripeClient) CreateCheckoutSession(p checkoutParams) (string, error) {
	f.lastCheckout = p
	f.checkoutCalls++
	if f.checkoutErr != nil {
		return "", f.checkoutErr
	}
	if f.checkoutURL == "" {
		return "https://checkout.stripe.com/c/pay/cs_test_fake", nil
	}
	return f.checkoutURL, nil
}

func (f *fakeStripeClient) CreateExpressAccount(email string) (string, error) {
	f.lastExpressEmail = email
	if f.expressErr != nil {
		return "", f.expressErr
	}
	if f.expressAccountID == "" {
		return "acct_fake123", nil
	}
	return f.expressAccountID, nil
}

func (f *fakeStripeClient) CreateAccountLink(accountID, refreshURL, returnURL string) (string, error) {
	f.lastAccountLinkID = accountID
	f.lastAccountLinkRefresh = refreshURL
	f.lastAccountLinkReturn = returnURL
	if f.accountLinkErr != nil {
		return "", f.accountLinkErr
	}
	if f.accountLinkURL == "" {
		return "https://connect.stripe.com/setup/fake", nil
	}
	return f.accountLinkURL, nil
}
