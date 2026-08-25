package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// App is a single catalog listing, parsed from CATALOG_DIR/<id>.json.
// Field set and semantics per SPEC.md §2.
type App struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Version       string   `json:"version"`
	Homepage      string   `json:"homepage"`
	Source        string   `json:"source"`
	Pkgname       string   `json:"pkgname"`
	PriceCents    int64    `json:"price_cents"`
	Currency      string   `json:"currency"`
	StripeAccount string   `json:"stripe_account"`
	Kind          string   `json:"kind"`
	Tags          []string `json:"tags"`
}

// LoadCatalog parses every *.json file directly under dir into an App.
// A file is skipped (logged, not fatal) if it fails to parse, its id is
// empty, its id doesn't match the filename, or it's priced but has no
// stripe_account. The returned slice is sorted by id for deterministic
// output. An error is only returned if dir itself can't be read.
func LoadCatalog(dir string, logger *log.Logger) ([]App, error) {
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading catalog dir %q: %w", dir, err)
	}

	var apps []App
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		wantID := strings.TrimSuffix(entry.Name(), ".json")

		raw, err := os.ReadFile(path)
		if err != nil {
			logger.Printf("catalog: skipping %s: %v", entry.Name(), err)
			continue
		}

		var app App
		if err := json.Unmarshal(raw, &app); err != nil {
			logger.Printf("catalog: skipping %s: invalid JSON: %v", entry.Name(), err)
			continue
		}

		if app.ID == "" {
			logger.Printf("catalog: skipping %s: empty id", entry.Name())
			continue
		}
		if app.ID != wantID {
			logger.Printf("catalog: skipping %s: id %q does not match filename", entry.Name(), app.ID)
			continue
		}
		if app.PriceCents > 0 && app.StripeAccount == "" {
			logger.Printf("catalog: skipping %s: price_cents > 0 requires stripe_account", entry.Name())
			continue
		}
		if app.PriceCents < 0 {
			logger.Printf("catalog: skipping %s: negative price_cents", entry.Name())
			continue
		}

		apps = append(apps, app)
	}

	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	return apps, nil
}
