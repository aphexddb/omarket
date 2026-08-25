package client_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestMain warms up a throwaway loopback connection before the suite runs.
// On some sandboxed Windows hosts the very first outbound 127.0.0.1
// connection in a fresh process occasionally stalls and fails (~1s, no
// retry) even though httptest.NewServer is already listening; every
// subsequent connection in the same process succeeds immediately. Eating
// that cold start here, rather than in a real test, keeps the suite
// deterministic regardless of test ordering.
func TestMain(m *testing.M) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 3; i++ {
		if resp, err := http.Get(srv.URL); err == nil {
			resp.Body.Close()
			break
		}
	}
	srv.Close()

	os.Exit(m.Run())
}
