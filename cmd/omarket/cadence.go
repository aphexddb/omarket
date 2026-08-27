package main

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"time"

	"github.com/aphexddb/omarket/client"
)

// Cadence bounds for the decay schedule used against a server that doesn't
// send an interval (an old server, or before the first response arrives).
// decayFloor is also the safety net that makes an ignored
// ?wait= harmless: no matter what, requests never come faster than this.
const (
	decayFloor      = 2 * time.Second
	decayCap        = 15 * time.Second
	decayMultiplier = 1.5
	decayJitter     = 0.2 // +/-20%
)

// nextDecayInterval steps the decay schedule: 2s, 3s, 4.5s, ... capped at
// 15s, never below the 2s floor. prev == 0 means "no interval issued yet",
// which starts the schedule at the floor. A pure function, deliberately
// separate from jitter() so both are independently testable.
func nextDecayInterval(prev time.Duration) time.Duration {
	if prev <= 0 {
		return decayFloor
	}
	next := time.Duration(float64(prev) * decayMultiplier)
	if next > decayCap {
		next = decayCap
	}
	if next < decayFloor {
		next = decayFloor
	}
	return next
}

// jitter applies +/-20% pseudo-random jitter to d, clamped back to
// decayFloor if the jitter would otherwise push it below the safety net.
func jitter(d time.Duration) time.Duration {
	factor := (1 - decayJitter) + rand.Float64()*(2*decayJitter)
	j := time.Duration(float64(d) * factor)
	if j < decayFloor {
		j = decayFloor
	}
	return j
}

// cadence tracks the polling gap for one buy/status wait: server-tunable
// when the server sends an interval, decaying with jitter
// otherwise. Zero value is ready to use.
type cadence struct {
	serverInterval time.Duration // >0 once the server has told us; sticky
	decayInterval  time.Duration // last decay step issued, for the no-interval path
}

// observe records a server-sent interval, if any (0 is "no opinion" and is
// ignored — this makes it safe to call after every response, including
// ones from an old server or a pending body that omitted the field).
func (cd *cadence) observe(serverInterval time.Duration) {
	if serverInterval > 0 {
		cd.serverInterval = serverInterval
	}
}

// current returns the cadence's most recent interval without advancing the
// decay schedule — used for 429 handling (max(Retry-After,
// interval)), where advancing would double-count the backoff.
func (cd *cadence) current() time.Duration {
	if cd.serverInterval > 0 {
		return cd.serverInterval
	}
	if cd.decayInterval > 0 {
		return cd.decayInterval
	}
	return decayFloor
}

// next returns the gap to wait before the next poll/long-poll re-issue,
// advancing (and jittering) the decay schedule if the server hasn't given
// us an authoritative interval.
func (cd *cadence) next() time.Duration {
	if cd.serverInterval > 0 {
		return cd.serverInterval
	}
	cd.decayInterval = nextDecayInterval(cd.decayInterval)
	return jitter(cd.decayInterval)
}

// maxConsecutiveTransportErrors bounds how many times in a row a status
// wait re-issues after a transport-level failure before giving up. One
// in-flight request dying is routine — the machine slept, the server
// redeployed mid-hold, a dial hiccuped — and the loop simply
// re-issues; a server that fails this many times in a row is genuinely
// unreachable and worth reporting instead of silently spinning.
const maxConsecutiveTransportErrors = 5

// pollRetrying calls poll, retrying the two failures a status wait must
// survive:
//
//   - 429 slow_down: sleep max(Retry-After, cadence's current
//     interval) and retry — 429 is never terminal for a status wait.
//   - Transport-level errors (no HTTP response at all): sleep the cadence's
//     current interval and retry, up to maxConsecutiveTransportErrors in a
//     row — an in-flight request dying mid-wait is an expected failure
//     mode, not a reason to abandon a purchase the server may be about to
//     complete.
//
// Any other HTTP error status, a cancelled ctx, or a non-429 success
// returns immediately.
func pollRetrying(ctx context.Context, cd *cadence, poll func() (status, key string, err error)) (string, string, error) {
	transportFailures := 0
	for {
		status, key, err := poll()
		if err == nil {
			return status, key, nil
		}

		var wait time.Duration
		var herr *client.HTTPError
		switch {
		case errors.As(err, &herr) && herr.StatusCode == http.StatusTooManyRequests:
			transportFailures = 0
			wait = herr.RetryAfter
			if cur := cd.current(); cur > wait {
				wait = cur
			}
		case errors.As(err, &herr):
			// The server answered; a non-429 error status is terminal here.
			return "", "", err
		case ctx.Err() != nil:
			// The caller's context ended (Ctrl-C, live budget): that's what
			// killed the request, not the network. Report the cancellation,
			// never retry past it.
			return "", "", ctx.Err()
		default:
			transportFailures++
			if transportFailures >= maxConsecutiveTransportErrors {
				return "", "", err
			}
			wait = cd.current()
		}

		if wait <= 0 {
			wait = decayFloor
		}
		select {
		case <-time.After(wait):
			continue
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
}
