package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// getWithRetry is http.Get with a few retries on transport-level failures.
// This machine intermittently times out fresh loopback dials under load
// (other processes churning sockets); one retry beat is enough to step past
// such a brownout without masking a listener that is genuinely down.
func getWithRetry(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		resp, err = http.Get(url)
		if err == nil {
			return resp, nil
		}
		t.Logf("GET %s attempt %d: %v", url, attempt+1, err)
	}
	return nil, err
}

func TestCallbackCorrectNonceWakes(t *testing.T) {
	cb := newCallbackListener()
	if cb == nil {
		t.Fatal("newCallbackListener returned nil (bind should succeed in a test environment)")
	}
	defer cb.close()

	resp, err := getWithRetry(t, fmt.Sprintf("http://127.0.0.1:%d/done?cb_nonce=%s", cb.port, cb.nonce))
	if err != nil {
		t.Fatalf("GET /done: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	select {
	case <-cb.wake:
	case <-time.After(2 * time.Second):
		t.Fatal("wake channel never closed after a correct-nonce hit")
	}
}

func TestCallbackWrongNonceDoesNotWake(t *testing.T) {
	cb := newCallbackListener()
	if cb == nil {
		t.Fatal("newCallbackListener returned nil")
	}
	defer cb.close()

	resp, err := getWithRetry(t, fmt.Sprintf("http://127.0.0.1:%d/done?cb_nonce=totally-wrong", cb.port))
	if err != nil {
		t.Fatalf("GET /done: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	select {
	case <-cb.wake:
		t.Fatal("wake channel closed after a wrong-nonce hit")
	case <-time.After(100 * time.Millisecond):
	}

	// The listener must still be alive for a later, legitimate hit.
	resp2, err := getWithRetry(t, fmt.Sprintf("http://127.0.0.1:%d/done?cb_nonce=%s", cb.port, cb.nonce))
	if err != nil {
		t.Fatalf("GET /done (retry with correct nonce): %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 (listener should still be up)", resp2.StatusCode)
	}
}

func TestCallbackMissingNonceDoesNotWake(t *testing.T) {
	cb := newCallbackListener()
	if cb == nil {
		t.Fatal("newCallbackListener returned nil")
	}
	defer cb.close()

	resp, err := getWithRetry(t, fmt.Sprintf("http://127.0.0.1:%d/done", cb.port))
	if err != nil {
		t.Fatalf("GET /done (no nonce): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	select {
	case <-cb.wake:
		t.Fatal("wake channel closed with no nonce at all")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCallbackCloseOnNilIsSafe(t *testing.T) {
	var cb *callbackListener
	cb.close() // must not panic
}

// TestNewCallbackOverridable checks the buy flow's injection point: tests
// (and, functionally, a real bind failure) can force newCallback to return
// nil so the caller proceeds without a listener.
func TestNewCallbackOverridable(t *testing.T) {
	orig := newCallback
	defer func() { newCallback = orig }()

	newCallback = func() *callbackListener { return nil }
	if got := newCallback(); got != nil {
		t.Fatalf("newCallback() = %v, want nil after override", got)
	}
}
