package client_test

import (
	"testing"

	"github.com/aphexddb/omarket/client"
)

func TestSellerTokenRoundTrip(t *testing.T) {
	setConfigDir(t, t.TempDir())

	if client.HasSellerToken() {
		t.Fatal("HasSellerToken true before any token saved")
	}
	tok, err := client.LoadSellerToken()
	if err != nil {
		t.Fatalf("LoadSellerToken (missing file): %v", err)
	}
	if tok != "" {
		t.Fatalf("token = %q, want empty before save", tok)
	}

	if err := client.SaveSellerToken("  sk_test_abc123  \n"); err != nil {
		t.Fatalf("SaveSellerToken: %v", err)
	}

	got, err := client.LoadSellerToken()
	if err != nil {
		t.Fatalf("LoadSellerToken: %v", err)
	}
	if got != "sk_test_abc123" {
		t.Fatalf("token = %q, want trimmed sk_test_abc123", got)
	}
	if !client.HasSellerToken() {
		t.Fatal("HasSellerToken false after saving")
	}
}
