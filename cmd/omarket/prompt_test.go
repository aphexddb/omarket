package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
)

// interactive builds a prompter fed by canned keystrokes, as if someone
// were sitting at a terminal.
func interactive(input string) (*prompter, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return &prompter{in: strings.NewReader(input), out: out, interactive: true}, out
}

// piped builds a prompter with no terminal attached — a CI run, a script,
// `omarket sell claim x < /dev/null`.
func piped() (*prompter, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return &prompter{in: strings.NewReader(""), out: out, interactive: false}, out
}

func TestPrompterConfirm(t *testing.T) {
	cases := []struct {
		name  string
		input string
		def   bool
		want  bool
	}{
		{"y", "y\n", false, true},
		{"yes", "yes\n", false, true},
		{"uppercase Y", "Y\n", false, true},
		{"padded yes", "  yes  \n", false, true},
		{"n", "n\n", true, false},
		{"no", "no\n", true, false},
		{"empty takes the default (no)", "\n", false, false},
		{"empty takes the default (yes)", "\n", true, true},
		{"anything else is not consent", "maybe\n", false, false},
		{"anything else does not override a no default", "sure thing\n", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, out := interactive(tc.input)
			got, err := p.confirm("Publish this?", tc.def)
			if err != nil {
				t.Fatalf("confirm: %v", err)
			}
			if got != tc.want {
				t.Fatalf("confirm(%q) = %v, want %v", tc.input, got, tc.want)
			}
			if !strings.Contains(out.String(), "Publish this?") {
				t.Errorf("the question was never shown: %q", out.String())
			}
		})
	}
}

// TestPrompterConfirmEOFIsNotConsent checks a closed stdin mid-prompt reads
// as "no", never as the default-yes. Silence is not agreement.
func TestPrompterConfirmEOFIsNotConsent(t *testing.T) {
	p, _ := interactive("")
	got, err := p.confirm("Publish this?", true)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if got {
		t.Fatal("EOF was treated as consent")
	}
}

// TestPrompterConfirmNoTerminal checks the non-interactive case reports
// that it could not ask, rather than quietly answering for the person.
func TestPrompterConfirmNoTerminal(t *testing.T) {
	p, out := piped()
	got, err := p.confirm("Publish this?", true)
	if !errors.Is(err, errNoTerminal) {
		t.Fatalf("err = %v, want errNoTerminal", err)
	}
	if got {
		t.Fatal("a question nobody could answer must not come back true")
	}
	if out.Len() != 0 {
		t.Errorf("nothing should be printed when there is no one to ask: %q", out.String())
	}
}

// TestResolveManifestAuthorPublicHandle checks a GitHub handle is used
// without pestering anyone: it is already public, and asking about it would
// train people to say yes to this prompt.
func TestResolveManifestAuthorPublicHandle(t *testing.T) {
	p, out := interactive("")
	got, note := resolveManifestAuthor(p, client.AuthorCandidate{
		Value: "aphexddb", Source: client.AuthorSourceGitHubUser,
	})
	if got != "aphexddb" {
		t.Fatalf("author = %q, want aphexddb", got)
	}
	if note == "" {
		t.Error("expected a note saying where the value came from")
	}
	if strings.Contains(out.String(), "?") {
		t.Errorf("a public handle should not raise a question: %q", out.String())
	}
}

// TestResolveManifestAuthorEmailConfirmed is the consent path: the CLI finds
// the address, says plainly that it would become public, and uses it only
// because the person said yes.
func TestResolveManifestAuthorEmailConfirmed(t *testing.T) {
	p, out := interactive("y\n")
	got, _ := resolveManifestAuthor(p, client.AuthorCandidate{
		Value: "someone@example.com", Source: client.AuthorSourceGitEmail,
	})
	if got != "someone@example.com" {
		t.Fatalf("author = %q, want the confirmed address", got)
	}
	shown := out.String()
	if !strings.Contains(shown, "someone@example.com") {
		t.Errorf("the prompt must show exactly what would be published: %q", shown)
	}
	if !strings.Contains(strings.ToLower(shown), "public") {
		t.Errorf("the prompt must say the value becomes public: %q", shown)
	}
}

// TestResolveManifestAuthorEmailDeclined is the point of the whole exercise:
// declining leaves the field empty, so nothing is published that wasn't
// agreed to.
func TestResolveManifestAuthorEmailDeclined(t *testing.T) {
	p, _ := interactive("n\n")
	got, note := resolveManifestAuthor(p, client.AuthorCandidate{
		Value: "someone@example.com", Source: client.AuthorSourceGitEmail,
	})
	if got != "" {
		t.Fatalf("author = %q, want empty after declining", got)
	}
	if note == "" {
		t.Error("expected a note explaining the field was left blank")
	}
}

// TestResolveManifestAuthorEmailNoTerminal checks the unattended case
// defaults to privacy: with nobody to ask, the address is not used.
func TestResolveManifestAuthorEmailNoTerminal(t *testing.T) {
	p, _ := piped()
	got, note := resolveManifestAuthor(p, client.AuthorCandidate{
		Value: "someone@example.com", Source: client.AuthorSourceGitEmail,
	})
	if got != "" {
		t.Fatalf("author = %q, want empty when consent could not be obtained", got)
	}
	if note == "" {
		t.Error("expected a note explaining why the field was left blank")
	}
}

// TestResolveManifestAuthorNothingFound checks the ordinary no-git case:
// empty field, no prompt, no fuss.
func TestResolveManifestAuthorNothingFound(t *testing.T) {
	p, out := interactive("")
	got, note := resolveManifestAuthor(p, client.AuthorCandidate{})
	if got != "" {
		t.Fatalf("author = %q, want empty", got)
	}
	if note != "" {
		t.Errorf("note = %q, want nothing to say", note)
	}
	if out.Len() != 0 {
		t.Errorf("nothing to ask about: %q", out.String())
	}
}

// TestConfirmPublishAuthor covers the second gate, at push time: a manifest
// whose author is email-shaped is about to put that address in a public
// catalog, so it gets one confirmation — however it got into the file.
func TestConfirmPublishAuthor(t *testing.T) {
	t.Run("a handle is never questioned", func(t *testing.T) {
		p, out := interactive("")
		if err := confirmPublishAuthor(p, "aphexddb", false); err != nil {
			t.Fatalf("confirmPublishAuthor: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("a handle should not raise a prompt: %q", out.String())
		}
	})

	t.Run("an email is confirmed", func(t *testing.T) {
		p, out := interactive("y\n")
		if err := confirmPublishAuthor(p, "someone@example.com", false); err != nil {
			t.Fatalf("confirmPublishAuthor: %v", err)
		}
		if !strings.Contains(out.String(), "someone@example.com") {
			t.Errorf("the prompt must show the address: %q", out.String())
		}
	})

	t.Run("declining stops the push", func(t *testing.T) {
		p, _ := interactive("n\n")
		if err := confirmPublishAuthor(p, "someone@example.com", false); err == nil {
			t.Fatal("declining must stop the push, not push anyway")
		}
	})

	t.Run("no terminal stops the push", func(t *testing.T) {
		p, _ := piped()
		err := confirmPublishAuthor(p, "someone@example.com", false)
		if err == nil {
			t.Fatal("an unattended push must not publish an unconfirmed address")
		}
		if !strings.Contains(err.Error(), "-yes") {
			t.Errorf("err = %q, want it to name the flag that unblocks a script", err)
		}
	})

	t.Run("-yes is explicit consent", func(t *testing.T) {
		p, out := piped()
		if err := confirmPublishAuthor(p, "someone@example.com", true); err != nil {
			t.Fatalf("confirmPublishAuthor with -yes: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("-yes means don't ask: %q", out.String())
		}
	})
}
