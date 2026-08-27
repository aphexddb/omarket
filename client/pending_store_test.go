package client_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aphexddb/omarket/client"
)

func TestPendingStoreRoundTrip(t *testing.T) {
	setConfigDir(t, t.TempDir())

	pending, err := client.ListPending()
	if err != nil {
		t.Fatalf("ListPending (empty): %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("ListPending (empty) = %+v, want none", pending)
	}

	now := time.Now()
	p := client.PendingPurchase{
		Token:     "pt_abc123",
		App:       "hello-shareware",
		Server:    "https://omarket.dev",
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(24 * time.Hour).Unix(),
	}
	if err := client.SavePending(p); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	got, err := client.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListPending = %+v, want 1 record", got)
	}
	if got[0] != p {
		t.Fatalf("ListPending[0] = %+v, want %+v", got[0], p)
	}

	if err := client.DeletePending(p.Token); err != nil {
		t.Fatalf("DeletePending: %v", err)
	}
	got, err = client.ListPending()
	if err != nil {
		t.Fatalf("ListPending after delete: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListPending after delete = %+v, want none", got)
	}

	// Deleting an already-gone record is not an error.
	if err := client.DeletePending(p.Token); err != nil {
		t.Fatalf("DeletePending (already gone): %v", err)
	}
}

func TestPendingStoreOldestFirst(t *testing.T) {
	setConfigDir(t, t.TempDir())

	base := time.Now()
	for i, tok := range []string{"pt_c", "pt_a", "pt_b"} {
		p := client.PendingPurchase{
			Token:     tok,
			App:       "app",
			Server:    "https://omarket.dev",
			CreatedAt: base.Add(time.Duration(-i) * time.Hour).Unix(),
			ExpiresAt: base.Add(24 * time.Hour).Unix(),
		}
		if err := client.SavePending(p); err != nil {
			t.Fatalf("SavePending(%s): %v", tok, err)
		}
	}

	got, err := client.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].CreatedAt > got[i].CreatedAt {
			t.Fatalf("ListPending not sorted oldest-first: %+v", got)
		}
	}
}

func TestListPendingCorruptFileSkipped(t *testing.T) {
	setConfigDir(t, t.TempDir())

	good := client.PendingPurchase{
		Token: "pt_good", App: "app", Server: "https://omarket.dev",
		CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := client.SavePending(good); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	dir, err := client.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	corruptPath := filepath.Join(dir, "pending", "pt_bad.json")
	if err := os.WriteFile(corruptPath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing corrupt record: %v", err)
	}

	got, err := client.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 1 || got[0].Token != "pt_good" {
		t.Fatalf("ListPending = %+v, want only pt_good (corrupt record should be skipped)", got)
	}
}

func TestListPendingMissingDir(t *testing.T) {
	setConfigDir(t, t.TempDir())

	got, err := client.ListPending()
	if err != nil {
		t.Fatalf("ListPending with no pending dir: %v", err)
	}
	if got != nil {
		t.Fatalf("ListPending with no pending dir = %+v, want nil", got)
	}
}
