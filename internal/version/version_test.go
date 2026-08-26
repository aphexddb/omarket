package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStringIncludesFields(t *testing.T) {
	Version, Commit, Date = "1.2.3", "deadbeef", "2026-01-02"
	got := String()
	if !strings.Contains(got, "1.2.3") || !strings.Contains(got, "deadbeef") || !strings.Contains(got, "2026-01-02") {
		t.Fatalf("String() = %q", got)
	}
}

func TestVERSIONFileIsSemver(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	v := strings.TrimSpace(string(b))
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(v) {
		t.Fatalf("VERSION = %q, want X.Y.Z", v)
	}
}
