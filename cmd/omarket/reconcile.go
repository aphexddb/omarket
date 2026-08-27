package main

import (
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/aphexddb/omarket/client"
)

// reconcileAndReport runs client.Reconcile and prints a one-line notice for
// every dropped record (SPEC §5.4). It never returns an error to the
// caller: reconciliation is a best-effort background courtesy on top of
// `omarket licenses`/the TUI launch, not something that should block or
// fail either of them. A ListPending error (a broken config dir, say) is
// swallowed the same way — the rest of the command still runs.
func reconcileAndReport(ctx context.Context, pub ed25519.PublicKey) {
	results, err := client.Reconcile(ctx, pub)
	if err != nil {
		return
	}
	for _, r := range results {
		if r.Outcome == client.OutcomeDropped && r.Notice != "" {
			fmt.Println(mutedStyle.Render(r.Notice))
		}
	}
}
