package client_test

import (
	"path/filepath"
	"testing"

	"github.com/aphexddb/omarket/client"
)

func withName(m client.Manifest, v string) client.Manifest        { m.Name = v; return m }
func withDescription(m client.Manifest, v string) client.Manifest { m.Description = v; return m }
func withHomepage(m client.Manifest, v string) client.Manifest    { m.Homepage = v; return m }
func withPrice(m client.Manifest, v int) client.Manifest          { m.PriceUSDCents = v; return m }
func withWare(m client.Manifest, v string) client.Manifest        { m.Ware = v; return m }
func withComment(m client.Manifest, v string) client.Manifest     { m.Comment = v; return m }
func withAuthor(m client.Manifest, v string) client.Manifest      { m.Author = v; return m }

func repeat(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func TestManifestIssues(t *testing.T) {
	valid := client.Manifest{
		ID:            "hello-shareware",
		Name:          "Hello Shareware",
		Description:   "A tiny terminal toy.",
		Homepage:      "https://hello.example.com",
		PriceUSDCents: 900,
		Ware:          "beerware",
		Comment:       "Buy me a beer if you like this tool. Cheers!",
		Author:        "aphexddb",
	}

	cases := []struct {
		name       string
		m          client.Manifest
		wantIssues bool
	}{
		{"valid manifest has no issues", valid, false},
		{"template name", withName(valid, "My App Name"), true},
		{"template description prefix", withDescription(valid, "One line about something"), true},
		{"template homepage", withHomepage(valid, "https://example.com"), true},
		{"zero price", withPrice(valid, 0), true},
		{"negative price", withPrice(valid, -5), true},
		{"fresh template has issues", client.NewManifestTemplate("hello-shareware"), true},

		// The ware trio. ware is optional and free-form; comment and author
		// are required, matching what the server enforces.
		{"empty ware is fine", withWare(valid, ""), false},
		{"invented ware is fine", withWare(valid, "sandwichware"), false},
		{"overlong ware", withWare(valid, repeat(client.MaxWareLen+1)), true},
		{"missing comment", withComment(valid, ""), true},
		{"whitespace comment", withComment(valid, "   "), true},
		{"too-short comment", withComment(valid, "hi"), true},
		{"overlong comment", withComment(valid, repeat(client.MaxCommentLen+1)), true},
		{"missing author", withAuthor(valid, ""), true},
		{"whitespace author", withAuthor(valid, "  "), true},
		{"overlong author", withAuthor(valid, repeat(client.MaxAuthorLen+1)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := client.ManifestIssues(tc.m)
			if (len(issues) > 0) != tc.wantIssues {
				t.Fatalf("ManifestIssues(%+v) = %v, want issues=%v", tc.m, issues, tc.wantIssues)
			}
		})
	}
}

func TestWriteManifestTemplateRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "omarket.json")

	if err := client.WriteManifestTemplate(path, "hello-shareware"); err != nil {
		t.Fatalf("WriteManifestTemplate: %v", err)
	}
	if err := client.WriteManifestTemplate(path, "hello-shareware"); err == nil {
		t.Fatal("expected error overwriting existing manifest")
	}

	m, err := client.ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.ID != "hello-shareware" {
		t.Fatalf("ID = %q, want hello-shareware", m.ID)
	}
	if len(client.ManifestIssues(m)) == 0 {
		t.Fatal("fresh template should have issues")
	}
}
