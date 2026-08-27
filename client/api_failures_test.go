package client_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aphexddb/omarket/client"
)

// serving stands up a test server running h and returns a Client pointed at
// it.
func serving(t *testing.T, h http.HandlerFunc) *client.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return client.NewClient(srv.URL)
}

// respond writes status with body and the given content type.
func respond(status int, contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// TestHTTPErrorCarriesStatusAndMessage checks the shape callers branch on:
// every non-2xx becomes an *HTTPError with the status intact, so a handler
// can tell "not configured" from "not authorized" without string matching.
func TestHTTPErrorCarriesStatusAndMessage(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge,
		http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
	}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := serving(t, respond(status, "application/json", `{"error":"the server said no"}`))

			_, err := c.GetCatalog(context.Background())
			var herr *client.HTTPError
			if !errors.As(err, &herr) {
				t.Fatalf("err = %v (%T), want *client.HTTPError", err, err)
			}
			if herr.StatusCode != status {
				t.Errorf("StatusCode = %d, want %d", herr.StatusCode, status)
			}
			if herr.Message != "the server said no" {
				t.Errorf("Message = %q, want the server's message", herr.Message)
			}
			if !strings.Contains(herr.Error(), "the server said no") {
				t.Errorf("Error() = %q, want it to include the server's message", herr.Error())
			}
		})
	}
}

// TestHTTPErrorNonJSONBodies is the case a naive JSON decode swallows: a
// proxy, load balancer, or captive portal answering with HTML or plain text.
// The status must survive and the message must say something, rather than
// leaving the user with a bare "unexpected status 502".
func TestHTTPErrorNonJSONBodies(t *testing.T) {
	cases := map[string]struct{ contentType, body string }{
		"html error page":         {"text/html", "<!doctype html><html><body><h1>502 Bad Gateway</h1></body></html>"},
		"plain text":              {"text/plain", "upstream connect error"},
		"empty body":              {"application/json", ""},
		"json but not ours":       {"application/json", `{"detail":"something else entirely"}`},
		"cloudflare problem+json": {"application/problem+json", `{"type":"https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-5xx-errors/error-502/","title":"Bad Gateway","status":502}`},
		"json null":               {"application/json", `null`},
		"json array":              {"application/json", `["nope"]`},
		"truncated json":          {"application/json", `{"error":"trunc`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := serving(t, respond(http.StatusBadGateway, tc.contentType, tc.body))

			_, err := c.GetCatalog(context.Background())
			var herr *client.HTTPError
			if !errors.As(err, &herr) {
				t.Fatalf("err = %v (%T), want *client.HTTPError", err, err)
			}
			if herr.StatusCode != http.StatusBadGateway {
				t.Errorf("StatusCode = %d, want 502", herr.StatusCode)
			}
			msg := herr.Error()
			if !strings.Contains(msg, "502") {
				t.Errorf("Error() = %q, want it to name the status", msg)
			}
			if strings.Contains(msg, "\n") || strings.Contains(msg, "\r") {
				t.Errorf("Error() = %q, want a single line", msg)
			}
			if strings.Contains(herr.Message, "developers.cloudflare.com") || strings.Contains(herr.Message, `"type"`) {
				t.Errorf("Message = %q, want no Cloudflare problem+json leak", herr.Message)
			}
		})
	}
}

// TestHTTPErrorTruncatesHugeBodies checks a server that answers an error
// with megabytes of anything can't turn into megabytes of terminal output.
func TestHTTPErrorTruncatesHugeBodies(t *testing.T) {
	huge := strings.Repeat("A", 5<<20)
	c := serving(t, respond(http.StatusInternalServerError, "text/plain", huge))

	_, err := c.GetCatalog(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := len(err.Error()); n > 1024 {
		t.Fatalf("error message is %d bytes; a server must not be able to flood the terminal", n)
	}
}

// TestHTTPErrorStripsControlCharacters checks a hostile error body can't
// repaint the terminal with ANSI escapes or scramble the line with control
// characters.
func TestHTTPErrorStripsControlCharacters(t *testing.T) {
	c := serving(t, respond(http.StatusBadRequest, "application/json",
		"{\"error\":\"bad\\u001b[31mred\\u0007\\ttabbed\\nnewline\"}"))

	_, err := c.GetCatalog(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, bad := range []string{"\x1b", "\a", "\n", "\r"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("error message %q still contains a control character %q", err.Error(), bad)
		}
	}
}

// TestSuccessBodyIsValidated is the other half of task-4 robustness: a 200
// with a body that isn't the promised shape must be an error naming the
// endpoint, not a silently zero-valued result.
func TestSuccessBodyIsValidated(t *testing.T) {
	cases := map[string]string{
		"truncated json": `{"apps": [`,
		"not json":       `<html>hello</html>`,
		"empty body":     ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c := serving(t, respond(http.StatusOK, "application/json", body))

			_, err := c.GetCatalog(context.Background())
			if err == nil {
				t.Fatal("expected an error for an undecodable success body")
			}
			if !strings.Contains(err.Error(), "/api/catalog.json") {
				t.Errorf("err = %q, want it to name the endpoint", err)
			}
		})
	}
}

// TestSuccessBodyIsTruncatedInErrors checks the same flood protection
// applies to a malformed *success* body as to an error one.
func TestSuccessBodyIsTruncatedInErrors(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", strings.Repeat("x", 5<<20)))

	_, err := c.GetCatalog(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := len(err.Error()); n > 1024 {
		t.Fatalf("error message is %d bytes, want it bounded", n)
	}
}

// TestNetworkErrorNamesTheServer checks the "is the server even up?" case
// points at the URL it tried, which is the one fact that makes a connection
// refused actionable (usually: the wrong -server, or a stale config).
func TestNetworkErrorNamesTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	c := client.NewClient(url)
	_, err := c.GetCatalog(context.Background())
	if err == nil {
		t.Fatal("expected an error against a closed server")
	}
	if !strings.Contains(err.Error(), url) {
		t.Errorf("err = %q, want it to name %s", err, url)
	}
}

// TestContextCancellationPropagates checks Ctrl-C during a slow request
// surfaces as context.Canceled rather than an opaque transport error, so
// callers can tell "the user stopped" from "the server broke".
func TestContextCancellationPropagates(t *testing.T) {
	release := make(chan struct{})
	c := serving(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := c.GetCatalog(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestHTTPErrorAdvice checks every status a caller can realistically see
// carries a next step. The exact wording is not pinned — that would make
// the test a copy of the code — but the presence of advice is, because an
// error without one is what sends a seller to the issue tracker.
func TestHTTPErrorAdvice(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge,
		http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
	}
	for _, status := range statuses {
		herr := &client.HTTPError{Method: http.MethodGet, Path: "/api/sellers/me", StatusCode: status}
		if herr.Advice() == "" {
			t.Errorf("status %d has no advice", status)
		}
	}
	// A status nobody has thought about yet gets no invented advice.
	odd := &client.HTTPError{Method: http.MethodGet, Path: "/x", StatusCode: 418}
	if odd.Advice() != "" {
		t.Errorf("status 418 advice = %q, want none", odd.Advice())
	}
}

// TestBuyFreeApp checks the ware-only response shape reaches the caller
// intact: no checkout URL, the free flag set, and the ware terms carried
// along so the CLI can show what's being asked.
func TestBuyFreeApp(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", `{
		"checkout_url": "",
		"purchase": "pt_free1",
		"free": true,
		"ware": "postcardware",
		"comment": "mail me a postcard",
		"author": "aphexddb"
	}`))

	got, err := c.Buy(context.Background(), client.BuyRequest{App: "postcard-cli"})
	if err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if !got.Free {
		t.Error("Free = false, want true")
	}
	if got.CheckoutURL != "" {
		t.Errorf("CheckoutURL = %q, want empty", got.CheckoutURL)
	}
	if got.Purchase != "pt_free1" {
		t.Errorf("Purchase = %q, want pt_free1", got.Purchase)
	}
	if got.Ware != "postcardware" || got.Comment != "mail me a postcard" || got.Author != "aphexddb" {
		t.Errorf("ware terms = %+v, want them carried through", got)
	}
}

// TestBuyPaidAppKeepsOldShape checks the free fields are additive: a paid
// response, which omits them entirely, still parses to a non-free result.
func TestBuyPaidAppKeepsOldShape(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json",
		`{"checkout_url":"https://checkout.stripe.com/x","purchase":"pt_paid1"}`))

	got, err := c.Buy(context.Background(), client.BuyRequest{App: "paid-app"})
	if err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if got.Free {
		t.Error("Free = true for a response that never mentioned it")
	}
	if got.CheckoutURL == "" || got.Purchase != "pt_paid1" {
		t.Errorf("got %+v, want the checkout URL and token", got)
	}
}

// TestBuyRejectsNonsenseFreeResponse checks the client refuses a response
// that claims a free purchase but hands back no way to collect the license.
// Left unchecked this would poll forever against a token that doesn't exist.
func TestBuyRejectsNonsenseFreeResponse(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", `{"checkout_url":"","purchase":"","free":true}`))

	_, err := c.Buy(context.Background(), client.BuyRequest{App: "broken"})
	if err == nil {
		t.Fatal("expected an error for a response with no purchase token")
	}
}

// TestBuyRejectsPaidResponseWithNoCheckoutURL is the same guard for the
// priced path: no URL and not free means there is nothing the CLI can do,
// and saying so beats printing an empty link.
func TestBuyRejectsPaidResponseWithNoCheckoutURL(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", `{"checkout_url":"","purchase":"pt_x"}`))

	_, err := c.Buy(context.Background(), client.BuyRequest{App: "broken"})
	if err == nil {
		t.Fatal("expected an error for a priced purchase with no checkout URL")
	}
}

// TestPollPurchaseRejectsUnknownStatus checks an unrecognized status doesn't
// silently read as "keep waiting" forever: the caller is told the server
// said something it doesn't understand.
func TestPollPurchaseRejectsUnknownStatus(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", `{"status":"refunded"}`))

	_, _, err := c.PollPurchase(context.Background(), "pt_x")
	if err == nil {
		t.Fatal("expected an error for an unrecognized purchase status")
	}
	if !strings.Contains(err.Error(), "refunded") {
		t.Errorf("err = %q, want it to quote the status it got", err)
	}
}

// TestPollPurchaseRejectsCompleteWithNoKey checks the one inconsistency that
// would otherwise be written to disk as a zero-byte license file.
func TestPollPurchaseRejectsCompleteWithNoKey(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", `{"status":"complete","license_key":""}`))

	_, _, err := c.PollPurchase(context.Background(), "pt_x")
	if err == nil {
		t.Fatal("expected an error for a complete purchase with no license key")
	}
}

// TestGetCatalogToleratesOddValues checks the client doesn't reject a
// catalog outright over one strange entry. Display code is responsible for
// rendering a negative price sanely (see formatCents in the CLI); the
// transport's job is to hand over what the server said.
func TestGetCatalogToleratesOddValues(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", `{"apps":[
		{"id":"negative","name":"Negative","price_cents":-1},
		{"id":"free","name":"Free","price_cents":0},
		{"id":"huge","name":"Huge","price_cents":9007199254740991}
	]}`))

	apps, err := c.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	if len(apps) != 3 {
		t.Fatalf("len(apps) = %d, want 3", len(apps))
	}
	if apps[0].PriceCents != -1 {
		t.Errorf("PriceCents = %d, want the value preserved for the caller to judge", apps[0].PriceCents)
	}
	if !apps[1].Free() {
		t.Error("a zero price should report Free()")
	}
	if apps[0].Free() {
		t.Error("a negative price is not free; it is broken")
	}
}

// TestGetCatalogRejectsNullApps checks a `null` apps array becomes an empty
// slice rather than something a caller has to nil-check.
func TestGetCatalogRejectsNullApps(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json", `{"apps":null}`))

	apps, err := c.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	if apps == nil {
		t.Fatal("apps = nil, want an empty slice")
	}
	if len(apps) != 0 {
		t.Fatalf("len(apps) = %d, want 0", len(apps))
	}
}

// TestRedirectsAreNotFollowedBlindlyToOtherHosts guards the one redirect
// that matters: an http server 302-ing a bearer-token request somewhere
// else would hand that token to whoever answers.
func TestRedirectsAreNotFollowedBlindlyToOtherHosts(t *testing.T) {
	var elsewhereGotAuth string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereGotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"seller_id":"sel_evil"}`))
	}))
	defer elsewhere.Close()

	c := serving(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/api/sellers/me", http.StatusFound)
	})

	_, err := c.GetSellerMe(context.Background(), "st_secret")
	if elsewhereGotAuth != "" {
		t.Fatalf("the seller token was forwarded to another host: %q", elsewhereGotAuth)
	}
	if err == nil {
		t.Fatal("expected a cross-host redirect to be refused")
	}
}

// TestUnexpectedStatusIsStillAnHTTPError checks a status nobody planned for
// (a 3xx that isn't a redirect the transport handles, say) still produces a
// typed error rather than a nil error and a zero value.
func TestUnexpectedStatusIsStillAnHTTPError(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusPartialContent, http.StatusNotModified} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := serving(t, respond(status, "application/json", ""))

			_, err := c.GetCatalog(context.Background())
			if err == nil {
				t.Fatalf("status %d produced no error", status)
			}
		})
	}
}
