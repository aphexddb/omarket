package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/aphexddb/omarket/client"
)

// apiError turns whatever came back from a server call into something the
// person at the terminal can act on. op names what was being attempted
// ("fetching stats", "pushing omarket.json").
//
// Three shapes get special treatment, because each has a different next
// step and the raw error tells them apart badly:
//
//   - a cancelled context is the person pressing Ctrl-C. That is not a
//     failure and must not be dressed up as one.
//   - a network error means the request never arrived, so nothing changed
//     on the server — worth saying, since "pushing failed" otherwise leaves
//     someone wondering whether half of it landed.
//   - an *HTTPError means the server answered and refused. Its own message
//     says what was wrong with this request; HTTPError.Advice adds what to
//     do about that class of refusal.
//
// Anything else is passed through wrapped, on the principle that an
// unrecognized error is better shown than paraphrased.
func apiError(op string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: cancelled", op)
	}

	var herr *client.HTTPError
	if errors.As(err, &herr) {
		msg := herr.Message
		if msg == "" {
			msg = fmt.Sprintf("the server returned %d %s", herr.StatusCode, http.StatusText(herr.StatusCode))
		}
		out := fmt.Sprintf("%s: %s", op, msg)
		if advice := herr.Advice(); advice != "" {
			out += "\n  " + advice
		}
		return errors.New(out)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%s: could not reach the server (%w)\n  nothing was changed — check your connection, or that -server points at the right market", op, err)
	}

	return fmt.Errorf("%s: %w", op, err)
}
