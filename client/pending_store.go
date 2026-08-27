package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const pendingFileMode = 0o600

// PendingGrace is added to a record's ExpiresAt before it is treated as
// truly expired (Reconcile, ListPending consumers), absorbing clock skew
// between this machine and the server.
const PendingGrace = 3600 // seconds (1h)

// PendingPurchase is a durable record of a purchase token whose outcome
// isn't known yet: the guarantee layer. It is saved to disk the
// moment Buy returns a token — before the checkout URL is even printed —
// so a purchase is never lost to a crash, Ctrl-C, sleep, or a timed-out
// live wait: `omarket licenses` and the TUI reconcile it later.
type PendingPurchase struct {
	Token     string `json:"token"`
	App       string `json:"app"`
	Server    string `json:"server"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// pendingDir returns ConfigDir()/pending.
func pendingDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pending"), nil
}

func pendingPath(token string) (string, error) {
	dir, err := pendingDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, token+".json"), nil
}

// SavePending writes p to pending/<token>.json (0600), creating the pending
// directory if needed.
func SavePending(p PendingPurchase) error {
	dir, err := pendingDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := pendingPath(p.Token)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, pendingFileMode)
}

// ListPending returns every stored pending purchase record, oldest first. A
// missing pending directory yields an empty (nil) slice, not an error.
// Files that fail to parse as JSON are silently skipped rather than
// aborting the whole listing — a single corrupt record shouldn't hide every
// other pending purchase.
func ListPending() ([]PendingPurchase, error) {
	dir, err := pendingDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []PendingPurchase
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p PendingPurchase
		if err := json.Unmarshal(b, &p); err != nil {
			continue // corrupt record: skip it, not the whole listing
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// DeletePending removes the pending record for token, if any. A missing
// file is not an error.
func DeletePending(token string) error {
	path, err := pendingPath(token)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
