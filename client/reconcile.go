package client

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// nowUnix is time.Now().Unix, indirected so expiry tests don't need to
// sleep in wall-clock time.
var nowUnix = func() int64 { return time.Now().Unix() }

// reconcileMaxRecords bounds how many pending records a single Reconcile
// call touches. Callers run it on every `omarket licenses`/TUI launch, so
// an unbounded scan over a long-neglected pending directory would make an
// unrelated command hang.
const reconcileMaxRecords = 5

// ReconcileOutcome classifies what Reconcile did with one pending record.
type ReconcileOutcome string

const (
	// OutcomeSaved: the purchase was complete; its license was verified and
	// saved, and the pending record was deleted.
	OutcomeSaved ReconcileOutcome = "saved"
	// OutcomeKept: still pending (or a network error, or a verification
	// failure) — the record is untouched, tried again next time.
	OutcomeKept ReconcileOutcome = "kept"
	// OutcomeDropped: the token is unknown to its server (404) or the
	// record has passed its expiry+grace window — the record was deleted.
	OutcomeDropped ReconcileOutcome = "dropped"
)

// ReconcileResult is the outcome for one pending record.
type ReconcileResult struct {
	Token   string
	App     string
	Outcome ReconcileOutcome
	// Notice is a one-line, user-facing message for OutcomeDropped results.
	// Empty otherwise.
	Notice string
	// Err is set for a kept result caused by a network error or a failed
	// license verification, for callers that want to log it. Never set
	// otherwise.
	Err error
}

// Reconcile resolves up to reconcileMaxRecords pending purchase records
// (oldest first), each against its own recorded server:
//
//   - complete            -> verify-then-save the license, delete the record
//   - pending              -> keep
//   - 404 (unknown token)  -> delete, with a notice
//   - past expiry+grace    -> delete, with a notice (no network call)
//   - network error        -> keep, silently (no notice; transient)
//
// pub is the public key used to verify a completed purchase's license
// before saving it (see VerifyThenSaveLicense).
func Reconcile(ctx context.Context, pub ed25519.PublicKey) ([]ReconcileResult, error) {
	records, err := ListPending()
	if err != nil {
		return nil, err
	}

	var results []ReconcileResult
	checked := 0
	for _, p := range records {
		if checked >= reconcileMaxRecords {
			break
		}

		if isExpired(p) {
			_ = DeletePending(p.Token)
			results = append(results, ReconcileResult{
				Token: p.Token, App: p.App, Outcome: OutcomeDropped,
				Notice: fmt.Sprintf("pending purchase of %s expired without completing; dropping it (%s)", p.App, p.Token),
			})
			continue
		}

		checked++
		results = append(results, reconcileOne(ctx, p, pub))
	}
	return results, nil
}

func isExpired(p PendingPurchase) bool {
	if p.ExpiresAt == 0 {
		return false // no expiry recorded (very old client write): never auto-drop on age alone
	}
	return nowUnix() > p.ExpiresAt+PendingGrace
}

func reconcileOne(ctx context.Context, p PendingPurchase, pub ed25519.PublicKey) ReconcileResult {
	c := NewClient(p.Server)
	status, key, err := c.PollPurchase(ctx, p.Token)
	if err != nil {
		var herr *HTTPError
		if errors.As(err, &herr) && herr.StatusCode == http.StatusNotFound {
			_ = DeletePending(p.Token)
			return ReconcileResult{
				Token: p.Token, App: p.App, Outcome: OutcomeDropped,
				Notice: fmt.Sprintf("pending purchase of %s is no longer known to %s; dropping it (%s)", p.App, p.Server, p.Token),
			}
		}
		// Network error (offline, DNS, timeout, 5xx, ...): keep it, quietly.
		// It's almost certainly transient and will resolve on a later run.
		return ReconcileResult{Token: p.Token, App: p.App, Outcome: OutcomeKept, Err: err}
	}

	if status != "complete" {
		return ReconcileResult{Token: p.Token, App: p.App, Outcome: OutcomeKept}
	}

	if _, err := VerifyThenSaveLicense(p.App, key, pub); err != nil {
		// Don't drop a completed purchase's record just because this
		// build's key doesn't verify it — keep it so a rebuild or a fixed
		// SHAREWARE_PUBLIC_KEY can still land it later.
		return ReconcileResult{Token: p.Token, App: p.App, Outcome: OutcomeKept, Err: err}
	}
	_ = DeletePending(p.Token)
	return ReconcileResult{Token: p.Token, App: p.App, Outcome: OutcomeSaved}
}
