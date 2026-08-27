package client_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		{"fresh template has issues", client.NewManifestTemplate("hello-shareware", "aphexddb"), true},

		// Price. Zero is the ware-only listing and is allowed; the band
		// between it and the floor is not, and neither is a negative.
		{"free price", withPrice(valid, 0), false},
		{"exactly the floor", withPrice(valid, client.MinPriceUSDCents), false},
		{"one cent", withPrice(valid, 1), true},
		{"just below the floor", withPrice(valid, client.MinPriceUSDCents-1), true},
		{"negative price", withPrice(valid, -5), true},
		{"absurdly negative price", withPrice(valid, -100000), true},
		{"above the cap", withPrice(valid, client.MaxPriceUSDCents+1), true},

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

// TestManifestIssues_FreePriceMentionsWare checks the free case is genuinely
// free rather than merely unvalidated: a zero-priced manifest with a ware and
// a comment is ready to push exactly as a priced one is.
func TestManifestIssues_FreePriceMentionsWare(t *testing.T) {
	m := client.Manifest{
		ID: "postcard-cli", Name: "Postcard CLI",
		Description: "Prints postcards.", Homepage: "https://example.org",
		PriceUSDCents: 0, Ware: "postcardware",
		Comment: "Mail me a postcard from wherever you are.", Author: "aphexddb",
	}
	if issues := client.ManifestIssues(m); len(issues) != 0 {
		t.Fatalf("a free postcardware manifest should be ready to push, got %v", issues)
	}
}

// TestPriceIssue covers the price rule on its own, including the messages —
// a negative price must not be told it could have been zero, because that
// advice hides the sign error that produced it.
func TestPriceIssue(t *testing.T) {
	cases := []struct {
		cents int
		ok    bool
	}{
		{0, true},
		{client.MinPriceUSDCents, true},
		{900, true},
		{client.MaxPriceUSDCents, true},
		{client.MaxPriceUSDCents + 1, false},
		{-1, false},
		{1, false},
		{client.MinPriceUSDCents - 1, false},
	}
	for _, tc := range cases {
		msg := client.PriceIssue(tc.cents)
		if (msg == "") != tc.ok {
			t.Errorf("PriceIssue(%d) = %q, want ok=%v", tc.cents, msg, tc.ok)
		}
	}

	negative := client.PriceIssue(-1)
	if negative == client.PriceIssue(client.MinPriceUSDCents-1) {
		t.Errorf("negative and below-floor share a message: %q", negative)
	}
	if !strings.Contains(negative, "negative") {
		t.Errorf("PriceIssue(-1) = %q, want it to name the problem", negative)
	}
}

func TestWriteManifestTemplateRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "omarket.json")

	if err := client.WriteManifestTemplate(path, "hello-shareware", "aphexddb"); err != nil {
		t.Fatalf("WriteManifestTemplate: %v", err)
	}
	if err := client.WriteManifestTemplate(path, "hello-shareware", "aphexddb"); err == nil {
		t.Fatal("expected error overwriting existing manifest")
	}

	m, err := client.ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.ID != "hello-shareware" {
		t.Fatalf("ID = %q, want hello-shareware", m.ID)
	}
	if m.Author != "aphexddb" {
		t.Fatalf("Author = %q, want aphexddb", m.Author)
	}
	if len(client.ManifestIssues(m)) == 0 {
		t.Fatal("fresh template should have issues")
	}
}

// TestWriteManifestTemplateBlankAuthor checks the privacy default: when
// nothing was confirmed, the template ships with an empty author rather than
// a guess, and ManifestIssues then makes filling it in a precondition of
// pushing.
func TestWriteManifestTemplateBlankAuthor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omarket.json")
	if err := client.WriteManifestTemplate(path, "hello-shareware", ""); err != nil {
		t.Fatalf("WriteManifestTemplate: %v", err)
	}
	m, err := client.ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.Author != "" {
		t.Fatalf("Author = %q, want empty when nothing was confirmed", m.Author)
	}

	var sawAuthor bool
	for _, issue := range client.ManifestIssues(m) {
		if strings.Contains(issue, "author") {
			sawAuthor = true
		}
	}
	if !sawAuthor {
		t.Fatal("a blank author must be reported as an issue, or it would push empty")
	}
}

func TestLooksLikeEmail(t *testing.T) {
	cases := map[string]bool{
		"aphexddb@gmail.com":       true,
		"first.last+tag@ex.co.uk":  true,
		"  spaced@example.com  ":   true,
		"aphexddb":                 false,
		"":                         false,
		"@example.com":             false,
		"nope@":                    false,
		"no-at-sign.example.com":   false,
		"two@at@signs.com":         false,
		"space in@example.com":     false,
		"nodot@localhost":          false,
		"Ada Lovelace":             false,
		"https://example.com/@who": false,
	}
	for in, want := range cases {
		if got := client.LooksLikeEmail(in); got != want {
			t.Errorf("LooksLikeEmail(%q) = %v, want %v", in, got, want)
		}
	}
}

// withGitConfig points git at a throwaway global config containing contents
// and moves the test into an empty directory, so the lookup can't pick up
// the developer's real identity or a repo-local override. Hermetic on
// purpose: this test is about what the CLI would publish, and reading the
// machine's actual email into an assertion would be the very leak under test.
func withGitConfig(t *testing.T, contents string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", path)
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(dir, "no-such-system-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Chdir(dir)
}

func TestGitAuthorCandidate(t *testing.T) {
	t.Run("github.user is a public handle", func(t *testing.T) {
		withGitConfig(t, "[github]\n\tuser = aphexddb\n[user]\n\temail = someone@example.com\n")

		got := client.GitAuthorCandidate()
		if got.Value != "aphexddb" {
			t.Fatalf("Value = %q, want aphexddb", got.Value)
		}
		if got.Source != client.AuthorSourceGitHubUser {
			t.Fatalf("Source = %q, want %q", got.Source, client.AuthorSourceGitHubUser)
		}
		if got.Private() {
			t.Fatal("a GitHub handle is already public; it should not need confirmation")
		}
	})

	t.Run("user.email is private", func(t *testing.T) {
		withGitConfig(t, "[user]\n\temail = someone@example.com\n")

		got := client.GitAuthorCandidate()
		if got.Value != "someone@example.com" {
			t.Fatalf("Value = %q, want someone@example.com", got.Value)
		}
		if got.Source != client.AuthorSourceGitEmail {
			t.Fatalf("Source = %q, want %q", got.Source, client.AuthorSourceGitEmail)
		}
		if !got.Private() {
			t.Fatal("an email address must be treated as private")
		}
	})

	t.Run("an email in github.user is still private", func(t *testing.T) {
		withGitConfig(t, "[github]\n\tuser = someone@example.com\n")

		got := client.GitAuthorCandidate()
		if !got.Private() {
			t.Fatal("Private() must look at the value, not only at which key it came from")
		}
	})

	t.Run("nothing configured", func(t *testing.T) {
		withGitConfig(t, "")

		got := client.GitAuthorCandidate()
		if got.Value != "" || got.Source != client.AuthorSourceNone {
			t.Fatalf("got %+v, want an empty candidate", got)
		}
		if got.Private() {
			t.Fatal("an empty candidate has nothing to disclose")
		}
	})
}
