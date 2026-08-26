package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ManifestFilename is the name of the per-app manifest file that
// `omarket sell claim` generates and `omarket sell push` reads, always in
// the current directory.
const ManifestFilename = "omarket.json"

// Manifest is the on-disk shape of omarket.json: the fields a seller edits
// locally before pushing to the server with PUT /api/apps/{id}.
//
// Ware, Comment and Author describe the listing's social contract: which
// "-ware" tradition it follows, the one-line ask that goes with it, and who
// is doing the asking. Ware is optional and defaults to "shareware"; the
// other two are required by the server.
type Manifest struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Homepage      string `json:"homepage"`
	PriceUSDCents int    `json:"price_usd_cents"`
	Ware          string `json:"ware"`
	Comment       string `json:"comment"`
	Author        string `json:"author"`
}

// Field limits, mirroring the server's. Checked here so a seller finds out
// before the round trip, not after — the server remains the trust boundary.
const (
	MaxWareLen    = 64
	MaxCommentLen = 140
	MaxAuthorLen  = 64
	MinCommentLen = 3
)

// DefaultWare is the tradition a listing follows when it doesn't name one:
// try it, pay if you keep it.
const DefaultWare = "shareware"

// WareSuggestion is one entry in the list of licensing traditions shown to
// a seller picking a ware.
type WareSuggestion struct{ Name, Blurb string }

// WareSuggestions is a list of well-known "-ware" traditions, offered as
// inspiration rather than a menu — the field is free-form precisely because
// the tradition is to invent your own. Kept in sync with the same list in
// the platform's operator CLI.
var WareSuggestions = []WareSuggestion{
	{"shareware", "try it, pay if you keep using it — the default"},
	{"beerware", "buy the author a beer if you like it"},
	{"coffeeware", "buy the author a coffee if it saved you an afternoon"},
	{"chocolateware", "send chocolate; surprisingly effective"},
	{"charityware", "pay what you like, but give it to a good cause"},
	{"pizzaware", "one slice per bug fixed feels fair"},
	{"postcardware", "mail the author a postcard from wherever you are"},
	{"careware", "do something kind for someone else instead of paying"},
	{"donationware", "free to use, donations keep it maintained"},
	{"nagware", "free forever, but it will remind you about this list"},
}

// Template placeholder values written by NewManifestTemplate. ManifestIssues
// flags any of these still present as not-yet-edited.
const (
	templateName            = "My App Name"
	templateDescriptionStub = "One line about"
	templateHomepage        = "https://example.com"
	templatePriceUSDCents   = 500
	templateCommentStub     = "What you ask of people"
)

// NewManifestTemplate returns the starter omarket.json contents for a
// freshly claimed app id. Author is pre-filled from git config when one can
// be found, since that's nearly always the right answer and saves the
// seller a lookup.
func NewManifestTemplate(id string) Manifest {
	return Manifest{
		ID:            id,
		Name:          templateName,
		Description:   templateDescriptionStub + " what your app does",
		Homepage:      templateHomepage,
		PriceUSDCents: templatePriceUSDCents,
		Ware:          DefaultWare,
		Comment:       templateCommentStub + " who use it",
		Author:        GitAuthor(),
	}
}

// GitAuthor guesses an author handle from local git config. github.user is
// preferred when set — it is literally the handle this field wants — with
// user.email as the fallback every git install has. Returns "" if git isn't
// available or has nothing to say, in which case the seller fills it in.
//
// The lookup is time-bounded: a misconfigured credential helper can make
// `git config` hang, and a convenience default is never worth blocking on.
func GitAuthor() string {
	for _, key := range []string{"github.user", "user.email"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		out, err := exec.CommandContext(ctx, "git", "config", "--get", key).Output()
		cancel()
		if err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				return v
			}
		}
	}
	return ""
}

// ManifestIssues returns human-readable reasons m is not ready to push: any
// unedited template placeholder, a non-positive price, or a ware trio that
// won't pass server validation. A nil/empty result means m is ready to push.
func ManifestIssues(m Manifest) []string {
	var issues []string
	if m.Name == templateName {
		issues = append(issues, fmt.Sprintf("name is still the template value %q", templateName))
	}
	if strings.HasPrefix(m.Description, templateDescriptionStub) {
		issues = append(issues, fmt.Sprintf("description is still the template value (starts with %q)", templateDescriptionStub))
	}
	if m.Homepage == templateHomepage {
		issues = append(issues, fmt.Sprintf("homepage is still the template value %q", templateHomepage))
	}
	if m.PriceUSDCents <= 0 {
		issues = append(issues, "price_usd_cents must be greater than 0")
	}
	if strings.HasPrefix(m.Comment, templateCommentStub) {
		issues = append(issues, fmt.Sprintf("comment is still the template value (starts with %q)", templateCommentStub))
	}
	issues = append(issues, wareIssues(m)...)
	return issues
}

// wareIssues checks the ware trio against the server's limits.
func wareIssues(m Manifest) []string {
	var issues []string
	if len(m.Ware) > MaxWareLen {
		issues = append(issues, fmt.Sprintf("ware must be at most %d characters", MaxWareLen))
	}
	switch comment := strings.TrimSpace(m.Comment); {
	case comment == "":
		issues = append(issues, "comment is required: say what your app asks of the people who use it")
	case len(comment) < MinCommentLen:
		issues = append(issues, fmt.Sprintf("comment must be at least %d characters", MinCommentLen))
	case len(m.Comment) > MaxCommentLen:
		issues = append(issues, fmt.Sprintf("comment must be at most %d characters", MaxCommentLen))
	}
	switch {
	case strings.TrimSpace(m.Author) == "":
		issues = append(issues, "author is required: your GitHub handle, or however you want to be credited")
	case len(m.Author) > MaxAuthorLen:
		issues = append(issues, fmt.Sprintf("author must be at most %d characters", MaxAuthorLen))
	}
	return issues
}

// WareOrDefault normalizes a ware value, substituting DefaultWare for an
// empty one, so a manifest that omits the field entirely still pushes.
func WareOrDefault(ware string) string {
	if strings.TrimSpace(ware) == "" {
		return DefaultWare
	}
	return strings.TrimSpace(ware)
}

// WriteManifestTemplate writes a template omarket.json for id to path,
// refusing to overwrite an existing file.
func WriteManifestTemplate(path, id string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; remove it or edit it directly", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := json.MarshalIndent(NewManifestTemplate(id), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ReadManifest reads and parses the manifest at path.
func ReadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return m, nil
}
