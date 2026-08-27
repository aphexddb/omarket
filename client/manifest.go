package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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

// Price limits, again mirroring the server's (see MinPriceUSDCents in the
// platform's validate.go). The three constants describe two allowed shapes
// and one closed band between them:
//
//   - FreePriceUSDCents: the listing asks for no money at all, and what it
//     does ask for lives in its ware and comment instead. Postcardware,
//     beerware, careware — the older half of the shareware tradition.
//   - MinPriceUSDCents up to MaxPriceUSDCents: an ordinary priced listing.
//   - anything strictly between free and the floor is refused. Zero means
//     "this is not about money"; a penny means "this is about money, but
//     card fees will eat all of it".
const (
	FreePriceUSDCents = 0
	MinPriceUSDCents  = 100
	MaxPriceUSDCents  = 100_000_000
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
// freshly claimed app id.
//
// author is passed in rather than looked up here, and that is the whole
// point: the only author worth pre-filling comes from git config, and the
// fallback git config always has is user.email. Writing that into a file
// destined for a public catalog is a disclosure, and a disclosure is the
// caller's to obtain consent for — see GitAuthorCandidate and
// AuthorCandidate.Private. An empty author is a perfectly good template;
// ManifestIssues will require it to be filled in before anything is pushed.
func NewManifestTemplate(id, author string) Manifest {
	return Manifest{
		ID:            id,
		Name:          templateName,
		Description:   templateDescriptionStub + " what your app does",
		Homepage:      templateHomepage,
		PriceUSDCents: templatePriceUSDCents,
		Ware:          DefaultWare,
		Comment:       templateCommentStub + " who use it",
		Author:        author,
	}
}

// AuthorSource names which git config key an author suggestion came from,
// so the caller can tell a handle meant for the world from an address that
// merely happened to be lying around.
type AuthorSource string

const (
	// AuthorSourceNone means git had nothing to offer.
	AuthorSourceNone AuthorSource = ""
	// AuthorSourceGitHubUser is github.user: a handle whose entire purpose
	// is to be public. Exactly what the author field wants.
	AuthorSourceGitHubUser AuthorSource = "github.user"
	// AuthorSourceGitEmail is user.email: the key every git install has,
	// and a personal contact address that its owner never agreed to publish.
	AuthorSourceGitEmail AuthorSource = "user.email"
)

// AuthorCandidate is a suggested author value together with where it came
// from, so a caller can decide whether using it needs the person's say-so.
type AuthorCandidate struct {
	Value  string
	Source AuthorSource
}

// Found reports whether git offered anything at all.
func (a AuthorCandidate) Found() bool { return strings.TrimSpace(a.Value) != "" }

// Private reports whether publishing Value would disclose something its
// owner did not choose to make public — in practice, an email address.
//
// It checks the value as well as the source. github.user is normally a
// handle, but nothing stops someone from having set it to their email, and
// a rule that trusted the key name alone would wave exactly that case
// through. The value is what gets published, so the value is what decides.
func (a AuthorCandidate) Private() bool {
	if !a.Found() {
		return false
	}
	return a.Source == AuthorSourceGitEmail || LooksLikeEmail(a.Value)
}

// emailPattern is a deliberately loose "is this an email address" check:
// something, an @, something with a dot in it, and no whitespace. It is not
// RFC 5322 and does not need to be — it decides whether to *ask* before
// publishing a value, so the only costly mistake is failing to recognize an
// address, and a false positive costs one confirmation prompt.
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s.]+(\.[^@\s.]+)+$`)

// LooksLikeEmail reports whether s is shaped like an email address.
func LooksLikeEmail(s string) bool {
	return emailPattern.MatchString(strings.TrimSpace(s))
}

// GitAuthorCandidate asks local git config for something to suggest as the
// author, and reports where the answer came from so the caller can treat a
// public handle and a private address differently. github.user is preferred
// — it is literally the handle this field wants — with user.email as the
// fallback every git install has. An empty candidate means git had nothing
// to say, in which case the seller fills the field in themselves.
//
// This function only *suggests*. It must never be wired straight into a
// manifest that gets pushed: see AuthorCandidate.Private.
//
// The lookup is time-bounded: a misconfigured credential helper can make
// `git config` hang, and a convenience default is never worth blocking on.
func GitAuthorCandidate() AuthorCandidate {
	for _, source := range []AuthorSource{AuthorSourceGitHubUser, AuthorSourceGitEmail} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		out, err := exec.CommandContext(ctx, "git", "config", "--get", string(source)).Output()
		cancel()
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(string(out)); v != "" {
			return AuthorCandidate{Value: v, Source: source}
		}
	}
	return AuthorCandidate{}
}

// PriceIssue checks a price against the rule described on FreePriceUSDCents
// and returns a human-readable reason it isn't acceptable, or "" if it is.
//
// A negative price gets its own message rather than the below-floor one:
// telling someone who has -500 in the field that they could also have used
// 0 describes the rule but buries the actual problem, which is a sign error
// somewhere upstream.
func PriceIssue(cents int) string {
	if cents == FreePriceUSDCents {
		return ""
	}
	if cents < 0 {
		return "price_usd_cents must not be negative"
	}
	if cents < MinPriceUSDCents {
		return fmt.Sprintf("price_usd_cents must be 0 (free — then the ware is the ask) or at least %d ($%.2f)",
			MinPriceUSDCents, float64(MinPriceUSDCents)/100)
	}
	if cents > MaxPriceUSDCents {
		return fmt.Sprintf("price_usd_cents must be at most %d", MaxPriceUSDCents)
	}
	return ""
}

// ManifestIssues returns human-readable reasons m is not ready to push: any
// unedited template placeholder, an unacceptable price, or a ware trio that
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
	if msg := PriceIssue(m.PriceUSDCents); msg != "" {
		issues = append(issues, msg)
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
// refusing to overwrite an existing file. author is written as-is; pass ""
// when nothing has been confirmed (see NewManifestTemplate).
func WriteManifestTemplate(path, id, author string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; remove it or edit it directly", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := json.MarshalIndent(NewManifestTemplate(id, author), "", "  ")
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
