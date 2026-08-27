package client_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aphexddb/omarket/client"
)

func catalogHandler(hits *int, apps []map[string]any, etag string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if etag != "" {
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apps": apps})
	}
}

func TestGetCatalogCachedFreshServesWithoutRequest(t *testing.T) {
	setConfigDir(t, t.TempDir())

	var hits int
	srv := httptest.NewServer(catalogHandler(&hits, []map[string]any{{"id": "a"}}, ""))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	if _, _, err := c.GetCatalogCached(context.Background()); err != nil {
		t.Fatalf("first GetCatalogCached: %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits after first call = %d, want 1", hits)
	}

	apps, stale, err := c.GetCatalogCached(context.Background())
	if err != nil {
		t.Fatalf("second GetCatalogCached: %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits after second (fresh-cached) call = %d, want still 1", hits)
	}
	if stale {
		t.Fatal("a fresh cache hit must not report stale")
	}
	if len(apps) != 1 || apps[0].ID != "a" {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestGetCatalogCached304TouchesWithoutRewrite(t *testing.T) {
	setConfigDir(t, t.TempDir())

	var hits int
	srv := httptest.NewServer(catalogHandler(&hits, []map[string]any{{"id": "a"}}, `"etag1"`))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	if _, _, err := c.GetCatalogCached(context.Background()); err != nil {
		t.Fatalf("first GetCatalogCached: %v", err)
	}

	// Force staleness by writing an old fetched_at directly, since the
	// public API has no way to backdate a fresh cache entry.
	backdateCatalogCache(t, srv.URL)

	apps, stale, err := c.GetCatalogCached(context.Background())
	if err != nil {
		t.Fatalf("second GetCatalogCached: %v", err)
	}
	if hits != 2 {
		t.Fatalf("hits after conditional GET = %d, want 2", hits)
	}
	if stale {
		t.Fatal("a 304 revalidation must not report stale")
	}
	if len(apps) != 1 || apps[0].ID != "a" {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestGetCatalogCached200Rewrites(t *testing.T) {
	setConfigDir(t, t.TempDir())

	var hits int
	apps := []map[string]any{{"id": "a"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{"apps": apps})
	}))
	defer srv.Close()

	c := client.NewClient(srv.URL)
	if _, _, err := c.GetCatalogCached(context.Background()); err != nil {
		t.Fatalf("first GetCatalogCached: %v", err)
	}
	backdateCatalogCache(t, srv.URL)

	apps[0] = map[string]any{"id": "b"} // catalog changed server-side
	got, stale, err := c.GetCatalogCached(context.Background())
	if err != nil {
		t.Fatalf("second GetCatalogCached: %v", err)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}
	if stale {
		t.Fatal("a fresh 200 must not report stale")
	}
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("expected the rewritten catalog, got %+v", got)
	}
}

func TestGetCatalogCachedOfflineServesStale(t *testing.T) {
	setConfigDir(t, t.TempDir())

	var hits int
	srv := httptest.NewServer(catalogHandler(&hits, []map[string]any{{"id": "a"}}, ""))
	c := client.NewClient(srv.URL)
	if _, _, err := c.GetCatalogCached(context.Background()); err != nil {
		t.Fatalf("first GetCatalogCached: %v", err)
	}
	backdateCatalogCache(t, srv.URL)
	srv.Close() // now offline

	apps, stale, err := c.GetCatalogCached(context.Background())
	if err != nil {
		t.Fatalf("GetCatalogCached while offline: %v", err)
	}
	if !stale {
		t.Fatal("offline fallback must report stale")
	}
	if len(apps) != 1 || apps[0].ID != "a" {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestGetCatalogCachedOfflineNoCacheErrors(t *testing.T) {
	setConfigDir(t, t.TempDir())

	c := client.NewClient("http://127.0.0.1:1") // nothing listening, and no prior cache
	if _, _, err := c.GetCatalogCached(context.Background()); err == nil {
		t.Fatal("expected an error with no cache and no reachable server")
	}
}

func TestGetCatalogCachedPerServerKeying(t *testing.T) {
	setConfigDir(t, t.TempDir())

	var hitsA, hitsB int
	srvA := httptest.NewServer(catalogHandler(&hitsA, []map[string]any{{"id": "a"}}, ""))
	defer srvA.Close()
	srvB := httptest.NewServer(catalogHandler(&hitsB, []map[string]any{{"id": "b"}}, ""))
	defer srvB.Close()

	appsA, _, err := client.NewClient(srvA.URL).GetCatalogCached(context.Background())
	if err != nil {
		t.Fatalf("GetCatalogCached A: %v", err)
	}
	appsB, _, err := client.NewClient(srvB.URL).GetCatalogCached(context.Background())
	if err != nil {
		t.Fatalf("GetCatalogCached B: %v", err)
	}
	if hitsA != 1 || hitsB != 1 {
		t.Fatalf("hitsA=%d hitsB=%d, want 1 each", hitsA, hitsB)
	}
	if appsA[0].ID != "a" || appsB[0].ID != "b" {
		t.Fatalf("cross-contaminated cache: appsA=%+v appsB=%+v", appsA, appsB)
	}
}

// backdateCatalogCache rewrites the cache entry for baseURL's fetched_at to
// well outside the freshness window, without touching its body/etag —
// simulating "the cache exists but needs revalidating" without waiting on
// a real clock.
func backdateCatalogCache(t *testing.T, baseURL string) {
	t.Helper()
	dir, err := client.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	sum := sha256.Sum256([]byte(baseURL))
	path := filepath.Join(dir, "cache", hex.EncodeToString(sum[:])+".json")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cache file %s: %v", path, err)
	}
	var e map[string]any
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("unmarshal cache entry: %v", err)
	}
	e["fetched_at"] = 0
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal cache entry: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("writing backdated cache file: %v", err)
	}
}
