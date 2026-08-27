package main

import (
	"context"
	"fmt"
	"io"

	"github.com/aphexddb/omarket/client"
)

// printWareAsk shows what a ware-only listing asks of the person who just
// took it.
//
// This is the entire consideration for a free acquisition, and the only
// moment it is guaranteed to be read: there is no receipt, no charge on a
// statement, nothing to come back to later. So it says three things and
// stops — that nothing was charged, which tradition the author picked, and
// the one line they wrote asking for whatever it is. A wall of text here
// would get skipped, and skipping is the failure mode that matters.
func printWareAsk(w io.Writer, res client.BuyResult) {
	ware := client.WareOrDefault(res.Ware)

	fmt.Fprintln(w)
	fmt.Fprintln(w, successStyle.Render("★ Yours — no charge. This is "+cell(ware)+". ★"))
	if comment := cell(res.Comment); comment != "" {
		fmt.Fprintln(w, "  "+wareBadge.Render(comment))
	}
	if author := cell(res.Author); author != "" {
		fmt.Fprintln(w, "  "+mutedStyle.Render("— "+author))
	}
	fmt.Fprintln(w)
}

// collectPurchase gets the license for a purchase that has just been
// started.
//
// A ware-only purchase is already complete before POST /api/buy answers, so
// it takes a single request. Sending it through the payment wait instead
// would work, but it would flash "waiting for payment..." and a spinner at
// someone who was never asked to pay — a small lie, and the one line of
// this flow most likely to be read.
//
// If that single request somehow comes back pending — an older server that
// predates inline free licensing, say — the normal wait takes over rather
// than failing. A server behaving unexpectedly should cost the buyer a
// spinner, not the app.
func collectPurchase(ctx context.Context, c *client.Client, res client.BuyResult, cd *cadence, cb *callbackListener) (status, key string, err error) {
	if res.Free {
		status, key, err = c.PollPurchase(ctx, res.Purchase)
		if err != nil {
			return "", "", apiError("collecting your license", err)
		}
		if status == client.PurchaseComplete {
			return status, key, nil
		}
	}
	return waitForPurchase(ctx, c, res.Purchase, cd, cb)
}
