package client_test

import (
	"testing"

	"github.com/aphexddb/omarchy-shareware/client"
)

func TestLicenseStoreRoundTrip(t *testing.T) {
	setConfigDir(t, t.TempDir())

	if client.HasLicense("app1") {
		t.Fatal("HasLicense true before any license saved")
	}

	if err := client.SaveLicense("app1", "  SHRW1.abc.def  \n"); err != nil {
		t.Fatalf("SaveLicense: %v", err)
	}

	key, err := client.LoadLicense("app1")
	if err != nil {
		t.Fatalf("LoadLicense: %v", err)
	}
	if key != "SHRW1.abc.def" {
		t.Fatalf("key = %q, want trimmed SHRW1.abc.def", key)
	}

	if !client.HasLicense("app1") {
		t.Fatal("HasLicense false after saving")
	}

	if err := client.SaveLicense("app2", "SHRW1.x.y"); err != nil {
		t.Fatalf("SaveLicense app2: %v", err)
	}

	entries, err := client.ListLicenses()
	if err != nil {
		t.Fatalf("ListLicenses: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2: %+v", len(entries), entries)
	}
	if entries[0].App != "app1" || entries[1].App != "app2" {
		t.Fatalf("entries not sorted by app: %+v", entries)
	}
}

func TestListLicensesEmpty(t *testing.T) {
	setConfigDir(t, t.TempDir())

	entries, err := client.ListLicenses()
	if err != nil {
		t.Fatalf("ListLicenses: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %+v, want empty", entries)
	}
}
