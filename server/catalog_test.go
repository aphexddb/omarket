package server

import (
	"log"
	"os"
	"path/filepath"
	"testing"
)

func writeCatalogFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestLoadCatalog(t *testing.T) {
	dir := t.TempDir()

	// Valid, free app.
	writeCatalogFile(t, dir, "hello.json", `{
		"id": "hello",
		"name": "Hello",
		"price_cents": 0,
		"currency": "usd"
	}`)

	// Valid, priced app with a stripe account.
	writeCatalogFile(t, dir, "paid.json", `{
		"id": "paid",
		"name": "Paid App",
		"price_cents": 999,
		"currency": "usd",
		"stripe_account": "acct_123"
	}`)

	// Invalid: priced but no stripe_account.
	writeCatalogFile(t, dir, "noaccount.json", `{
		"id": "noaccount",
		"name": "No Account",
		"price_cents": 500,
		"currency": "usd"
	}`)

	// Invalid: id doesn't match filename.
	writeCatalogFile(t, dir, "mismatch.json", `{
		"id": "not-mismatch",
		"name": "Mismatch",
		"price_cents": 0
	}`)

	// Invalid: empty id.
	writeCatalogFile(t, dir, "empty.json", `{
		"id": "",
		"name": "Empty",
		"price_cents": 0
	}`)

	// Invalid: malformed JSON.
	writeCatalogFile(t, dir, "broken.json", `{ this is not json`)

	// Ignored: not a .json file.
	writeCatalogFile(t, dir, "README.md", `not a catalog entry`)

	logger := log.New(os.Stderr, "test: ", 0)
	apps, err := LoadCatalog(dir, logger)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if len(apps) != 2 {
		t.Fatalf("expected 2 valid apps, got %d: %+v", len(apps), apps)
	}
	if apps[0].ID != "hello" || apps[1].ID != "paid" {
		t.Fatalf("expected sorted [hello, paid], got [%s, %s]", apps[0].ID, apps[1].ID)
	}
}

func TestLoadCatalog_MissingDir(t *testing.T) {
	_, err := LoadCatalog(filepath.Join(t.TempDir(), "does-not-exist"), nil)
	if err == nil {
		t.Fatal("expected error for missing catalog dir")
	}
}
