package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"sync"
	"time"
)

// callbackDoneHTML is the tiny static page served on a valid /done hit. It
// never mentions the license key: the callback is a wake-up hint only, not
// a delivery channel (SPEC §3.5, §7.2).
const callbackDoneHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>omarket</title></head>
<body style="font-family: sans-serif; text-align: center; margin-top: 3em;">
<p>payment received - return to the terminal</p>
</body></html>
`

// callbackListener is the loopback callback server (RFC 8252 §7.3 style)
// the success page redirects to after checkout, waking the buy command
// early instead of it waiting out its next poll interval (SPEC §3.5, §5.3).
// It is a hint, never an authority: a valid hit only closes wake, which the
// caller treats as a cue to re-check with the server, never as proof of
// completion on its own.
type callbackListener struct {
	port  int
	nonce string

	wake     chan struct{}
	wakeOnce sync.Once

	srv *http.Server
	ln  net.Listener
}

// newCallback attempts to stand up a callback listener. Every failure mode
// — can't generate a nonce, can't bind 127.0.0.1:0 — is silent: it returns
// nil, and the caller proceeds without callback fields on the buy request
// (layer 2, long-poll, takes over). Indirected through a package var so
// tests can force the failure path.
var newCallback = newCallbackListener

func newCallbackListener() *callbackListener {
	nonce, err := randomNonce()
	if err != nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil
	}

	cl := &callbackListener{
		nonce: nonce,
		wake:  make(chan struct{}),
		ln:    ln,
	}
	cl.port = ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("GET /done", cl.handleDone)
	cl.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = cl.srv.Serve(ln) // returns http.ErrServerClosed on close(); nothing to report
	}()
	return cl
}

// randomNonce returns a 128-bit crypto/rand nonce, base64url-encoded
// without padding (SPEC §7.3).
func randomNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// handleDone serves the callback's only route. It never logs the query
// string (the nonce transits it) — a wrong or missing nonce gets a bodyless
// 404 and the listener stays alive for a legitimate later hit; a correct
// one gets the static page and wakes the waiter exactly once.
func (cl *callbackListener) handleDone(w http.ResponseWriter, r *http.Request) {
	got := r.URL.Query().Get("cb_nonce")
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(cl.nonce)) != 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(callbackDoneHTML))

	cl.wakeOnce.Do(func() { close(cl.wake) })

	// Shut down after responding, off the handler goroutine so the
	// response above isn't cut short.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = cl.srv.Shutdown(ctx)
	}()
}

// close shuts the listener down if it's still up. Safe to call on a nil
// *callbackListener (bind failed, or no callback was requested) and safe to
// call more than once.
func (cl *callbackListener) close() {
	if cl == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = cl.srv.Shutdown(ctx)
}
