package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/aphexddb/omarket/client"
)

// TestNextDecayIntervalSequence pins the exact decay schedule (SPEC §5.2):
// 2s, 3s, 4.5s, 6.75s, 10.125s, capped at 15s from then on.
func TestNextDecayIntervalSequence(t *testing.T) {
	want := []time.Duration{
		2 * time.Second,
		3 * time.Second,
		4500 * time.Millisecond,
		6750 * time.Millisecond,
		10125 * time.Millisecond,
		15 * time.Second,
		15 * time.Second, // stays capped
	}
	var got time.Duration
	for i, w := range want {
		got = nextDecayInterval(got)
		if got != w {
			t.Fatalf("step %d: nextDecayInterval = %v, want %v", i, got, w)
		}
	}
}

// TestNextDecayIntervalNeverBelowFloor checks the floor holds even from an
// unusual starting point (e.g. a manually reset cadence).
func TestNextDecayIntervalNeverBelowFloor(t *testing.T) {
	for _, prev := range []time.Duration{0, -time.Second, 500 * time.Millisecond} {
		if got := nextDecayInterval(prev); got < decayFloor {
			t.Errorf("nextDecayInterval(%v) = %v, below the %v floor", prev, got, decayFloor)
		}
	}
}

// TestJitterBounds checks jitter stays within +/-20% and never below the
// floor, across many samples (it's randomized).
func TestJitterBounds(t *testing.T) {
	d := 10 * time.Second
	lo := time.Duration(float64(d) * 0.8)
	hi := time.Duration(float64(d) * 1.2)
	for i := 0; i < 1000; i++ {
		j := jitter(d)
		if j < lo || j > hi {
			t.Fatalf("jitter(%v) = %v, outside [%v, %v]", d, j, lo, hi)
		}
	}

	// A small base must still respect the floor even after -20% jitter.
	for i := 0; i < 1000; i++ {
		if j := jitter(decayFloor); j < decayFloor {
			t.Fatalf("jitter(decayFloor) = %v, below the floor", j)
		}
	}
}

// TestCadenceServerIntervalSticky checks a server-sent interval overrides
// decay entirely and stays in effect (SPEC §5.2).
func TestCadenceServerIntervalSticky(t *testing.T) {
	cd := &cadence{}
	cd.observe(5 * time.Second)
	for i := 0; i < 3; i++ {
		if got := cd.next(); got != 5*time.Second {
			t.Fatalf("next() = %v, want the sticky server interval 5s", got)
		}
	}
	// A later zero (old-server-shaped response) must not clear it.
	cd.observe(0)
	if got := cd.next(); got != 5*time.Second {
		t.Fatalf("next() after observe(0) = %v, want still 5s", got)
	}
}

// TestCadenceDecayWithoutServerInterval checks the cadence sequence itself
// (not just the pure step function) tracks the decay schedule and never
// drops below the floor — this stands in for a real-time "spacing never
// drops below the floor against an old server" test, without an actual
// multi-minute wall-clock run.
func TestCadenceDecayWithoutServerInterval(t *testing.T) {
	// next() jitters the (already capped) decay step by up to +20%, so the
	// jittered value's own ceiling is the cap plus that margin, not the cap
	// itself.
	jitteredCeiling := time.Duration(float64(decayCap) * 1.2)

	cd := &cadence{}
	for i := 0; i < 6; i++ {
		got := cd.next()
		if got < decayFloor {
			t.Fatalf("step %d: cadence.next() = %v, below the floor", i, got)
		}
		if got > jitteredCeiling {
			t.Fatalf("step %d: cadence.next() = %v, above the jittered ceiling %v", i, got, jitteredCeiling)
		}
	}
}

// TestPollRetrying429ThenCompletes scripts {429, 429, pending, complete}: a
// status wait must survive repeated slow_down responses and still complete,
// honoring RetryAfter each time rather than giving up (SPEC §3.4).
func TestPollRetrying429ThenCompletes(t *testing.T) {
	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/purchase/pt_limited", func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1, 2:
			w.Header().Set("Retry-After", "0") // keep the test fast
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
		case 3:
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "complete", "license_key": "SHRW1.a.b"})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.NewClient(srv.URL)
	cd := &cadence{}
	cd.observe(10 * time.Millisecond) // keep the "pending" retry gap fast too; only exercised outside this helper

	status, key, err := pollRetrying(context.Background(), cd, func() (string, string, error) {
		return c.PollPurchase(context.Background(), "pt_limited")
	})
	if err != nil {
		t.Fatalf("pollRetrying: %v", err)
	}
	// pollRetrying only retries failures (429, transport); a "pending"
	// result at call 3 is a legitimate return, not one it retries. Call it
	// again to reach the "complete" response, simulating the outer wait
	// loop's own re-issue.
	if status == "pending" {
		status, key, err = pollRetrying(context.Background(), cd, func() (string, string, error) {
			return c.PollPurchase(context.Background(), "pt_limited")
		})
		if err != nil {
			t.Fatalf("pollRetrying (second call): %v", err)
		}
	}
	if status != "complete" || key != "SHRW1.a.b" {
		t.Fatalf("status=%q key=%q, want complete/SHRW1.a.b", status, key)
	}
	if calls != 4 {
		t.Fatalf("calls = %d, want 4 (429, 429, pending, complete)", calls)
	}
}

// TestPollRetrying429UsesRetryAfterFloor checks the sleep between retries is
// at least max(RetryAfter, cadence's current interval), not less.
func TestPollRetrying429UsesRetryAfterFloor(t *testing.T) {
	var calls int
	var timestamps []time.Time
	mux := http.NewServeMux()
	mux.HandleFunc("/api/purchase/pt_limited", func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0") // RetryAfter=0: the cadence floor should still apply
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "complete", "license_key": "SHRW1.a.b"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.NewClient(srv.URL)
	cd := &cadence{}
	cd.observe(80 * time.Millisecond)

	_, _, err := pollRetrying(context.Background(), cd, func() (string, string, error) {
		return c.PollPurchase(context.Background(), "pt_limited")
	})
	if err != nil {
		t.Fatalf("pollRetrying: %v", err)
	}
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap < 75*time.Millisecond { // small margin below the 80ms floor for scheduler jitter
			t.Fatalf("retry %d..%d gap = %v, want at least ~%v (cadence floor)", i-1, i, gap, 80*time.Millisecond)
		}
	}
}

// TestPollRetryingTransientTransportErrors checks a wait survives requests
// that die without an HTTP response — the machine slept, the server
// redeployed mid-hold, a dial hiccuped (SPEC §11) — by re-issuing rather
// than aborting the whole wait.
func TestPollRetryingTransientTransportErrors(t *testing.T) {
	cd := &cadence{}
	cd.observe(5 * time.Millisecond) // keep retry gaps fast

	var calls int
	status, key, err := pollRetrying(context.Background(), cd, func() (string, string, error) {
		calls++
		if calls < 3 {
			return "", "", &url.Error{Op: "Get", URL: "http://127.0.0.1:1/x", Err: errors.New("connectex: connection timed out")}
		}
		return "complete", "SHRW1.a.b", nil
	})
	if err != nil {
		t.Fatalf("pollRetrying: %v", err)
	}
	if status != "complete" || key != "SHRW1.a.b" {
		t.Fatalf("status=%q key=%q, want complete/SHRW1.a.b", status, key)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (fail, fail, complete)", calls)
	}
}

// TestPollRetryingTransportErrorsExhaust checks a genuinely unreachable
// server still surfaces its error after maxConsecutiveTransportErrors
// attempts in a row, instead of spinning until the outer budget.
func TestPollRetryingTransportErrorsExhaust(t *testing.T) {
	cd := &cadence{}
	cd.observe(5 * time.Millisecond)

	var calls int
	dialErr := &url.Error{Op: "Get", URL: "http://127.0.0.1:1/x", Err: errors.New("connectex: connection timed out")}
	_, _, err := pollRetrying(context.Background(), cd, func() (string, string, error) {
		calls++
		return "", "", dialErr
	})
	if !errors.Is(err, dialErr) {
		t.Fatalf("err = %v, want the underlying transport error", err)
	}
	if calls != maxConsecutiveTransportErrors {
		t.Fatalf("calls = %d, want %d", calls, maxConsecutiveTransportErrors)
	}
}

// TestPollRetryingCancelNotRetried checks a cancelled context is reported as
// the cancellation (Ctrl-C must abort a wait), never retried as if it were
// a network blip.
func TestPollRetryingCancelNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cd := &cadence{}
	cd.observe(5 * time.Millisecond)

	var calls int
	_, _, err := pollRetrying(ctx, cd, func() (string, string, error) {
		calls++
		cancel()
		return "", "", &url.Error{Op: "Get", URL: "http://x", Err: context.Canceled}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry past cancellation)", calls)
	}
}
