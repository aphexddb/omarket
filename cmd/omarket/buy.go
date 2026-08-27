package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aphexddb/omarket/client"
	"github.com/mdp/qrterminal/v3"
)

// Timing knobs for the layered wait. Vars, not
// consts, so tests can shrink them instead of running for real minutes.
var (
	buyLiveBudget       = 10 * time.Minute // wall-clock cap on the whole wait, independent of expires_in
	buyPhaseADuration   = 10 * time.Second // plain-poll phase: payment normally takes longer than this
	buyLongPollWait     = 25 * time.Second // per-request ?wait= value once phase A ends (server clamps to <=25s anyway)
	buyFastPollWindow   = 30 * time.Second // after a wake, how long to fast-poll if still pending
	buyFastPollInterval = 1 * time.Second  // cadence during that fast-poll window
)

// defaultPendingTTL is used when the server doesn't send expires_in (an old
// server): matches Stripe Checkout's default session lifetime.
const defaultPendingTTL = 24 * time.Hour

var spinnerFrames = []rune{'|', '/', '-', '\\'}

// runBuy implements `omarket buy [app] [-email x]`: with no app argument it
// browses the catalog (absorbing the old `list` command); with one, it runs
// the purchase flow.
func runBuy(args []string) error {
	fs := flag.NewFlagSet("buy", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	email := fs.String("email", "", "buyer email (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return printCatalog(*server)
	}
	appID := fs.Arg(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	serverURL := client.ResolveServer(*server)
	c := client.NewClient(serverURL)

	// Loopback callback (layer 1): best-effort. A nil cb (nonce/bind
	// failure) just means the buy request carries no callback fields, and
	// the wait degrades straight to long-poll after phase A.
	cb := newCallback()
	defer cb.close()

	req := client.BuyRequest{App: appID, Email: *email}
	if cb != nil {
		req.CallbackPort = cb.port
		req.CallbackNonce = cb.nonce
	}

	res, err := c.Buy(ctx, req)
	if err != nil {
		return buyStartError(appID, err)
	}
	return completePurchase(ctx, c, appID, serverURL, res, cb)
}

// runCheckout finishes a purchase the TUI already started (QR + wait +
// save). It must not POST /api/buy again — that would open a second
// checkout session. No loopback listener here: the TUI's buy carried no
// callback fields, so the wait leans on long-poll and the pending record.
func runCheckout(server, appID string, res client.BuyResult) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return completePurchase(ctx, client.NewClient(server), appID, server, res, nil)
}

// completePurchase runs everything after POST /api/buy has answered:
// persist the pending record, show the checkout UI, drive the layered wait,
// and verify-then-save the license. Shared by `omarket buy` and the TUI's
// checkout handoff.
func completePurchase(ctx context.Context, c *client.Client, appID, serverURL string, res client.BuyResult, cb *callbackListener) error {
	if err := persistPending(res, appID, serverURL); err != nil {
		// Not fatal: the live wait below still runs. Only the
		// crash/Ctrl-C/timeout recovery guarantee is weaker for this one
		// purchase, and that's worth a warning, not aborting a purchase the
		// buyer is about to pay for.
		fmt.Fprintln(os.Stderr, "warning: could not save pending purchase record:", err)
	}

	// A ware-only (free) app has nothing to check out: the server already
	// signed the license, so the very first poll below returns it. Only a
	// priced purchase gets the checkout URL/QR/wait UI.
	if res.Free {
		fmt.Println()
		if res.Comment != "" {
			fmt.Println(wareBadge.Render(res.Comment))
		}
	} else {
		fmt.Println()
		fmt.Println(checkoutStyle.Render("Checkout: " + res.CheckoutURL))
		fmt.Println()
		qrterminal.GenerateHalfBlock(res.CheckoutURL, qrterminal.M, os.Stdout)
		fmt.Println()
		fmt.Println(mutedStyle.Render("Waiting for payment... (Ctrl-C to cancel)"))
	}

	cd := &cadence{}
	cd.observe(res.Interval)

	status, key, err := waitForPurchase(ctx, c, res.Purchase, cd, cb)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			printStillPending()
			return nil
		}
		return err
	}
	if status != client.PurchaseComplete {
		// Live budget elapsed. Not a failure: the pending record saved
		// above makes this recoverable via `omarket licenses`.
		printStillPending()
		return nil
	}

	pub, err := resolvePublicKey()
	if err != nil {
		return err
	}
	if _, err := client.VerifyThenSaveLicense(appID, key, pub); err != nil {
		return err
	}
	_ = client.DeletePending(res.Purchase)

	dir, _ := client.LicensesDir()
	path := filepath.Join(dir, appID+".key")

	fmt.Println()
	fmt.Println(successStyle.Render("★ Registered! Thanks for supporting the developer. ★"))
	fmt.Printf("License key saved to %s\n", path)
	return nil
}

func printStillPending() {
	fmt.Println(mutedStyle.Render("Purchase still pending - the key lands automatically; check later with `omarket licenses`."))
}

// persistPending saves the pending-purchase record immediately after Buy
// returns a token, before anything else is printed — so a Ctrl-C at the QR
// screen already loses nothing.
func persistPending(res client.BuyResult, appID, serverURL string) error {
	ttl := res.ExpiresIn
	if ttl <= 0 {
		ttl = defaultPendingTTL
	}
	now := time.Now()
	return client.SavePending(client.PendingPurchase{
		Token:     res.Purchase,
		App:       appID,
		Server:    serverURL,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	})
}

const buyUnavailableMsg = "this listing isn't accepting payments right now"

// edgeErrorPage matches Cloudflare's bare origin-error body ("error code:
// 502"). The client quotes non-JSON error bodies into HTTPError.Message, so
// this page arrives as a Message rather than an empty string — but it is
// the edge talking, not the API, and deserves the same friendly fallback.
var edgeErrorPage = regexp.MustCompile(`^error code: \d+$`)

// isEdgeError reports whether msg is the CDN talking, not sharewared.
// Cloudflare has two 502 bodies we have actually seen: plain
// "error code: 502", and RFC 7807 problem+json whose type URL is
// developers.cloudflare.com — the one that printed as
// `Couldn't buy omarket — {"type":"https://developers.cloudflare.com/...`.
func isEdgeError(msg string) bool {
	if msg == "" || msg == "failed to create checkout session" {
		return true
	}
	if edgeErrorPage.MatchString(msg) {
		return true
	}
	if strings.Contains(msg, "developers.cloudflare.com") {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(msg), "{") {
		return true
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype")
}

// buyStartError turns a failed POST /api/buy into a buyer-facing sentence.
// Cloudflare rewrites origin 502s into an error page (plain text or
// problem+json), which is why a raw HTTPError used to leak into the TUI.
func buyStartError(appID string, err error) error {
	var herr *client.HTTPError
	if !errors.As(err, &herr) {
		return fmt.Errorf("couldn't buy %s: %w", appID, err)
	}
	switch herr.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%q isn't in the catalog", appID)
	case http.StatusConflict, http.StatusBadGateway, http.StatusServiceUnavailable:
		msg := herr.Message
		if isEdgeError(msg) {
			msg = buyUnavailableMsg
		}
		return fmt.Errorf("couldn't buy %s: %s", appID, msg)
	}
	if herr.Message != "" {
		return fmt.Errorf("couldn't buy %s: %s", appID, herr.Message)
	}
	return fmt.Errorf("couldn't buy %s: %w", appID, err)
}

// waitForPurchase drives the layered wait until the purchase completes, the
// live budget (buyLiveBudget) elapses, or ctx is cancelled.
// Returns status=="complete" with a key on success; status=="pending"
// with a nil error on a budget timeout (not a failure); a non-nil error on
// cancellation or a hard request failure.
func waitForPurchase(ctx context.Context, c *client.Client, token string, cd *cadence, cb *callbackListener) (status, key string, err error) {
	deadline := time.Now().Add(buyLiveBudget)
	phaseAEnd := time.Now().Add(buyPhaseADuration)
	cbArmed := cb != nil // one-shot: disarmed after the first wake is handled

	frame := 0
	spin := func() {
		fmt.Printf("\r%s %s", string(spinnerFrames[frame%len(spinnerFrames)]), mutedStyle.Render("waiting for payment..."))
		frame++
	}

	pollNow := func() (string, string, error) {
		return pollRetrying(ctx, cd, func() (string, string, error) {
			return c.PollPurchase(ctx, token)
		})
	}
	waitNow := func() (string, string, error) {
		return pollRetrying(ctx, cd, func() (string, string, error) {
			s, k, iv, err := c.WaitPurchase(ctx, token, buyLongPollWait)
			if err == nil {
				cd.observe(iv)
			}
			return s, k, err
		})
	}

	for time.Now().Before(deadline) {
		spin()

		var perr error
		if time.Now().Before(phaseAEnd) {
			status, key, perr = pollNow()
		} else {
			status, key, perr = waitNow()
		}
		if perr != nil {
			fmt.Println()
			return "", "", fmt.Errorf("checking purchase status: %w", perr)
		}
		if status == "complete" {
			fmt.Print("\r")
			return status, key, nil
		}

		var wakeCh <-chan struct{}
		if cbArmed {
			wakeCh = cb.wake
		}
		select {
		case <-wakeCh:
			cbArmed = false // consume: a closed channel would otherwise fire on every future select
			status, key, err = wokenFastPoll(ctx, pollNow, deadline, spin)
			if err != nil {
				fmt.Println()
				return "", "", err
			}
			if status == "complete" {
				fmt.Print("\r")
				return status, key, nil
			}
		case <-time.After(cd.next()):
		case <-ctx.Done():
			fmt.Println()
			return "", "", ctx.Err()
		}
	}

	fmt.Println()
	return "pending", "", nil
}

// wokenFastPoll runs the authoritative re-check triggered by a loopback
// wake, then — if the redirect outran the webhook — 1s-interval fast polls
// for buyFastPollWindow before handing back to the caller's normal cadence.
func wokenFastPoll(ctx context.Context, pollNow func() (string, string, error), deadline time.Time, spin func()) (string, string, error) {
	spin()
	status, key, err := pollNow()
	if err != nil {
		return "", "", fmt.Errorf("checking purchase status: %w", err)
	}
	if status == "complete" {
		return status, key, nil
	}

	fastDeadline := time.Now().Add(buyFastPollWindow)
	for time.Now().Before(fastDeadline) && time.Now().Before(deadline) {
		select {
		case <-time.After(buyFastPollInterval):
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
		spin()
		status, key, err = pollNow()
		if err != nil {
			return "", "", fmt.Errorf("checking purchase status: %w", err)
		}
		if status == "complete" {
			return status, key, nil
		}
	}
	return status, key, nil // still pending; caller resumes its normal cadence
}
