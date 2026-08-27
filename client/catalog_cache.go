package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// catalogCacheFresh is how long a cached catalog is trusted with zero
// network requests (SPEC §5.5/§3.6).
const catalogCacheFresh = 5 * time.Minute

// catalogCacheEntry is the on-disk shape of one cache file: the last
// catalog response body, its ETag (if the server sent one), and when it was
// last confirmed fresh.
type catalogCacheEntry struct {
	ETag      string          `json:"etag"`
	FetchedAt int64           `json:"fetched_at"`
	Body      json.RawMessage `json:"body"`
}

func catalogCacheDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache"), nil
}

// catalogCacheKey hashes baseURL so one client config directory can cache
// catalogs from several servers without collisions.
func catalogCacheKey(baseURL string) string {
	sum := sha256.Sum256([]byte(baseURL))
	return hex.EncodeToString(sum[:])
}

func catalogCachePath(baseURL string) (string, error) {
	dir, err := catalogCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, catalogCacheKey(baseURL)+".json"), nil
}

func loadCatalogCache(baseURL string) *catalogCacheEntry {
	path, err := catalogCachePath(baseURL)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil // missing (or unreadable): treat as a cache miss
	}
	var e catalogCacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil // corrupt: treat as a miss
	}
	return &e
}

func saveCatalogCache(baseURL string, e catalogCacheEntry) error {
	dir, err := catalogCacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := catalogCachePath(baseURL)
	if err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func parseCatalogBody(body []byte) ([]App, error) {
	var out catalogResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Apps, nil
}

// fetchCatalogRaw issues one GET /api/catalog.json, sending If-None-Match
// when etag is non-empty. It returns the raw body, status code, and any
// ETag the server sent back.
func (c *Client) fetchCatalogRaw(ctx context.Context, etag string) (body []byte, status int, respETag string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/catalog.json", nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, "", err
	}
	return b, resp.StatusCode, resp.Header.Get("ETag"), nil
}

// GetCatalogCached is GetCatalog backed by a disk cache keyed on the
// client's server URL (SPEC §5.5):
//
//   - a cache written within the last catalogCacheFresh -> returned as-is,
//     zero requests.
//   - otherwise a conditional GET is issued (If-None-Match, when an ETag is
//     cached): 304 -> the cache is just touched (fetched_at bumped, body
//     kept); 200 -> the cache is rewritten.
//   - a network error with a cache on disk (of any age) serves that stale
//     cache instead of failing; stale reports true so callers can add a
//     muted note. A network error with no cache at all is a real error.
//
// GetCatalog itself is untouched; call it directly for tests or an explicit
// uncached fetch.
func (c *Client) GetCatalogCached(ctx context.Context) (apps []App, stale bool, err error) {
	cached := loadCatalogCache(c.BaseURL)

	if cached != nil && time.Since(time.Unix(cached.FetchedAt, 0)) < catalogCacheFresh {
		if out, perr := parseCatalogBody(cached.Body); perr == nil {
			return out, false, nil
		}
		cached = nil // corrupt cached body: fall through to a real fetch
	}

	etag := ""
	if cached != nil {
		etag = cached.ETag
	}
	body, status, respETag, reqErr := c.fetchCatalogRaw(ctx, etag)
	if reqErr != nil {
		if cached != nil {
			if out, perr := parseCatalogBody(cached.Body); perr == nil {
				return out, true, nil // offline: serve stale rather than fail
			}
		}
		return nil, false, reqErr
	}

	if status == http.StatusNotModified && cached != nil {
		_ = saveCatalogCache(c.BaseURL, catalogCacheEntry{ETag: cached.ETag, FetchedAt: time.Now().Unix(), Body: cached.Body})
		if out, perr := parseCatalogBody(cached.Body); perr == nil {
			return out, false, nil
		}
	}
	if status != http.StatusOK {
		return nil, false, &HTTPError{Method: http.MethodGet, Path: "/api/catalog.json", StatusCode: status}
	}

	out, perr := parseCatalogBody(body)
	if perr != nil {
		return nil, false, perr
	}
	_ = saveCatalogCache(c.BaseURL, catalogCacheEntry{ETag: respETag, FetchedAt: time.Now().Unix(), Body: body})
	return out, false, nil
}
