package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
)

// TestAPIErrorNil checks the pass-through case, so callers can wrap
// unconditionally.
func TestAPIErrorNil(t *testing.T) {
	if err := apiError("doing a thing", nil); err != nil {
		t.Fatalf("apiError(nil) = %v, want nil", err)
	}
}

// TestAPIErrorHTTPStatuses checks every status a seller can realistically
// hit produces a message naming the operation, quoting the server, and
// offering a next step.
func TestAPIErrorHTTPStatuses(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge,
		http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
	}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			err := apiError("pushing omarket.json", &client.HTTPError{
				Method: http.MethodPut, Path: "/api/apps/x", StatusCode: status,
				Message: "price_usd_cents must not be negative",
			})
			got := err.Error()
			if !strings.Contains(got, "pushing omarket.json") {
				t.Errorf("err = %q, want it to name the operation", got)
			}
			if !strings.Contains(got, "price_usd_cents must not be negative") {
				t.Errorf("err = %q, want it to quote the server", got)
			}
			if !strings.Contains(got, "\n  ") {
				t.Errorf("err = %q, want an indented next step", got)
			}
		})
	}
}

// TestAPIErrorHTTPNoMessage checks a server that refuses with an empty body
// still produces something better than a bare number.
func TestAPIErrorHTTPNoMessage(t *testing.T) {
	err := apiError("fetching stats", &client.HTTPError{
		Method: http.MethodGet, Path: "/api/sellers/stats", StatusCode: http.StatusBadGateway,
	})
	got := err.Error()
	if !strings.Contains(got, "502") {
		t.Errorf("err = %q, want the status code", got)
	}
	if !strings.Contains(got, "Bad Gateway") {
		t.Errorf("err = %q, want the status text", got)
	}
}

// TestAPIErrorUnknownStatusHasNoInventedAdvice checks a status with no
// sensible generic advice doesn't get a made-up one.
func TestAPIErrorUnknownStatusHasNoInventedAdvice(t *testing.T) {
	err := apiError("fetching stats", &client.HTTPError{
		Method: http.MethodGet, Path: "/x", StatusCode: 418, Message: "i am a teapot",
	})
	if strings.Contains(err.Error(), "\n  ") {
		t.Errorf("err = %q, want no invented advice for an unmapped status", err)
	}
}

// TestAPIErrorCancelled checks Ctrl-C reads as cancellation, not failure.
func TestAPIErrorCancelled(t *testing.T) {
	err := apiError("waiting", fmt.Errorf("get: %w", context.Canceled))
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("err = %q, want it to say cancelled", err)
	}
	if strings.Contains(err.Error(), "context") {
		t.Errorf("err = %q, want the Go plumbing kept out of it", err)
	}
}

// fakeNetError is a net.Error, which is how the transport reports an
// unreachable or unresponsive server.
type fakeNetError struct{}

func (fakeNetError) Error() string   { return "dial tcp 127.0.0.1:1: connection refused" }
func (fakeNetError) Timeout() bool   { return false }
func (fakeNetError) Temporary() bool { return true }

// TestAPIErrorNetwork checks an unreachable server says so, and says
// explicitly that nothing changed — the fact a seller actually needs before
// deciding whether to retry a push.
func TestAPIErrorNetwork(t *testing.T) {
	var _ net.Error = fakeNetError{}

	err := apiError("pushing omarket.json", fmt.Errorf("PUT /api/apps/x: %w", fakeNetError{}))
	got := err.Error()
	if !strings.Contains(got, "could not reach the server") {
		t.Errorf("err = %q, want it to name the reachability problem", got)
	}
	if !strings.Contains(got, "nothing was changed") {
		t.Errorf("err = %q, want it to say nothing changed", got)
	}
}

// TestAPIErrorPassthrough checks an unrecognized error is shown rather than
// paraphrased, and stays unwrappable.
func TestAPIErrorPassthrough(t *testing.T) {
	sentinel := errors.New("something nobody anticipated")
	err := apiError("doing a thing", sentinel)
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want it to wrap the original", err)
	}
	if !strings.Contains(err.Error(), "something nobody anticipated") {
		t.Errorf("err = %q, want the original text", err)
	}
}
