package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func testApps() []client.App {
	return []client.App{
		{
			ID: "hello-shareware", Name: "Hello Shareware", Version: "1.0.0",
			Description: "A tiny demo app that says hello and asks nothing in return",
			Ware:        "", PriceCents: 0,
		},
		{
			ID: "super-grep-deluxe-professional", Name: "Super Grep Deluxe Professional Edition",
			Version: "2.3.1", Author: "Ada",
			Description: "Searches everything, everywhere, all at once, with a description long enough to need truncation on any reasonable terminal width because it just keeps going",
			Ware:        "beerware", PriceCents: 500,
			Comment: "Buy me a beer if this saved your afternoon.",
			Tags:    []string{"search", "cli"},
		},
		{
			ID: "postcard-notes", Name: "Postcard Notes", Version: "0.9",
			Description: "Notes that travel", Ware: "postcardware", PriceCents: 12345,
		},
	}
}

func testModel(w, h int) model {
	m := newModel("https://omarket.dev")
	m.apps = testApps()
	m.loaded = true
	m.width, m.height = w, h
	m.applyFilter()
	return m
}

func TestTruncCell(t *testing.T) {
	tests := []struct {
		s    string
		w    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly-10", 10, "exactly-10"},
		{"this is too long", 10, "this is t…"},
		{"anything", 0, ""},
		{"wide", 1, "…"},
	}
	for _, tt := range tests {
		if got := truncCell(tt.s, tt.w); got != tt.want {
			t.Errorf("truncCell(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
		}
		if got := truncCell(tt.s, tt.w); lipgloss.Width(got) > tt.w {
			t.Errorf("truncCell(%q, %d) = %q: wider than %d", tt.s, tt.w, got, tt.w)
		}
	}
}

func TestPadCellWidth(t *testing.T) {
	for _, s := range []string{"", "abc", "a much longer string than the column"} {
		if got := lipgloss.Width(padCell(s, 12)); got != 12 {
			t.Errorf("padCell(%q, 12) width = %d, want 12", s, got)
		}
	}
}

func TestSplitLine(t *testing.T) {
	got := splitLine("left", "right", 20)
	if lipgloss.Width(got) != 20 {
		t.Errorf("splitLine width = %d, want 20", lipgloss.Width(got))
	}
	if !strings.HasPrefix(got, "left") || !strings.HasSuffix(got, "right") {
		t.Errorf("splitLine = %q", got)
	}
	// Too narrow: keep left, drop right rather than overflow weirdly.
	if got := splitLine("left", "right", 8); got != "left" {
		t.Errorf("splitLine narrow = %q, want %q", got, "left")
	}
}

// TestRenderListFitsWidth is the core layout invariant: no rendered line may
// exceed the terminal width, at any size.
func TestRenderListFitsWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120} {
		m := testModel(w, 24)
		for i, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: line %d is %d cells: %q", w, i, got, ansi.Strip(line))
			}
		}
	}
}

func TestRenderDetailFitsWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120} {
		m := testModel(w, 24)
		m.state = stateDetail
		m.detail = &m.apps[1] // longest name, comment, tags
		for i, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: line %d is %d cells: %q", w, i, got, ansi.Strip(line))
			}
		}
	}
}

func TestRenderListShowsWareAndName(t *testing.T) {
	plain := ansi.Strip(testModel(100, 24).View())
	for _, want := range []string{
		"Hello Shareware", "shareware", // default ware filled in
		"beerware", "postcardware",
		"NAME", "WARE", "PRICE", "DESCRIPTION",
		"FREE", "$5.00", "$123.45",
		"3/3 apps", "omarket.dev",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("list view missing %q:\n%s", want, plain)
		}
	}
}

func TestRenderListHeightMatchesTerminal(t *testing.T) {
	for _, apps := range []int{0, 3} {
		m := testModel(80, 24)
		m.apps = m.apps[:apps]
		m.applyFilter()
		// View is wrapped in docStyle (1-line top/bottom margins), so the
		// full render must come to exactly the terminal height.
		if got := strings.Count(m.View(), "\n") + 1; got != 24 {
			t.Errorf("%d apps: view is %d lines, want 24", apps, got)
		}
	}
}

func TestRenderDetailShowsWareTrio(t *testing.T) {
	m := testModel(80, 24)
	m.state = stateDetail
	m.detail = &m.apps[1]
	plain := ansi.Strip(m.View())
	for _, want := range []string{
		"Super Grep Deluxe Professional Edition", "v2.3.1",
		"beerware", "by Ada", "$5.00",
		"Buy me a beer if this saved your afternoon.",
		"search, cli",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("detail view missing %q:\n%s", want, plain)
		}
	}
}

func TestFormatNotice(t *testing.T) {
	got := formatNotice("couldn't buy omarket: this listing isn't accepting payments right now")
	want := "Couldn't buy omarket — this listing isn't accepting payments right now"
	if got != want {
		t.Fatalf("formatNotice = %q, want %q", got, want)
	}
}

func TestBuyFailureStaysInTUI(t *testing.T) {
	m := testModel(80, 24)
	m.cursor = 1 // paid app
	next, cmd := m.handleListKey(teaKey("b"))
	got, ok := next.(model)
	if !ok {
		t.Fatalf("handleListKey(b) returned %T", next)
	}
	if cmd == nil {
		t.Fatal("buy should start a command, not quit")
	}
	if got.action != nil {
		t.Fatal("buy must not leave the TUI before checkout starts")
	}
	if got.buying != "super-grep-deluxe-professional" {
		t.Fatalf("buying = %q", got.buying)
	}

	updated, quit := got.Update(buyResultMsg{
		app: "super-grep-deluxe-professional",
		err: buyStartError("omarket", &client.HTTPError{
			Method: "POST", Path: "/api/buy", StatusCode: 502,
			Message: `{"type":"https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-5xx-errors/error-502/"}`,
		}),
	})
	got = updated.(model)
	if quit != nil {
		t.Fatal("a failed buy must not quit the TUI")
	}
	if got.action != nil {
		t.Fatal("a failed buy must not set a post-TUI action")
	}
	if got.buying != "" {
		t.Fatalf("buying still set: %q", got.buying)
	}
	plain := ansi.Strip(got.View())
	if !strings.Contains(plain, "Couldn't buy omarket") {
		t.Fatalf("status line missing notice:\n%s", plain)
	}
	if !strings.Contains(plain, "this listing isn't accepting payments right now") {
		t.Fatalf("status line missing reason:\n%s", plain)
	}
	if strings.Contains(plain, "error:") {
		t.Fatalf("status line still has CLI 'error:' prefix:\n%s", plain)
	}
	if strings.Contains(plain, "unexpected status") {
		t.Fatalf("status line leaked HTTP internals:\n%s", plain)
	}
	if strings.Contains(plain, "cloudflare") || strings.Contains(plain, `"type"`) {
		t.Fatalf("status line leaked Cloudflare JSON:\n%s", plain)
	}

	cleared, _ := got.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got = cleared.(model)
	if got.notice != "" {
		t.Fatalf("esc should dismiss notice, still %q", got.notice)
	}
}

func TestInstallStartsWithOmarchy(t *testing.T) {
	defer stubInstallVia("omarchy")()
	m := testModel(80, 24)
	m.apps[0].Pkgname = "hello-shareware"
	m.applyFilter()

	next, cmd := m.handleListKey(teaKey("i"))
	got := mustModel(t, next)
	if cmd == nil {
		t.Fatal("install should start a command, not quit")
	}
	if got.action != nil {
		t.Fatal("install must not leave the TUI")
	}
	if got.installing != "hello-shareware" {
		t.Fatalf("installing = %q", got.installing)
	}
	if got.installPkg != "hello-shareware" {
		t.Fatalf("installPkg = %q, want the catalog pkgname", got.installPkg)
	}
	if got.installVia != "omarchy" {
		t.Fatalf("installVia = %q, want omarchy first", got.installVia)
	}
	plain := ansi.Strip(got.View())
	if !strings.Contains(plain, "Installing hello-shareware via omarchy") {
		t.Fatalf("status line should say it's using omarchy first:\n%s", plain)
	}
	if strings.Contains(plain, "sudo") || strings.Contains(plain, "pacman -S") {
		t.Fatalf("status line leaked a console install command:\n%s", plain)
	}
}

func TestInstallOmarchyMissTriesPacman(t *testing.T) {
	m := testModel(80, 24)
	m.installing = "hello-shareware"
	m.installPkg = "hello-shareware"
	m.installVia = "omarchy"

	updated, cmd := m.Update(installResultMsg{
		app:     "hello-shareware",
		pkg:     "hello-shareware",
		via:     "omarchy",
		tryNext: "pacman",
	})
	got := mustModel(t, updated)
	if cmd == nil {
		t.Fatal("omarchy miss should start a pacman attempt, not stop")
	}
	if got.action != nil {
		t.Fatal("falling through to pacman must not leave the TUI")
	}
	if got.installing != "hello-shareware" {
		t.Fatalf("installing = %q, want still in-flight", got.installing)
	}
	if got.installVia != "pacman" {
		t.Fatalf("installVia = %q, want pacman", got.installVia)
	}
	if got.notice != "" {
		t.Fatalf("should not show an error while retrying via pacman, notice=%q", got.notice)
	}
	plain := ansi.Strip(got.View())
	if !strings.Contains(plain, "Installing hello-shareware via pacman") {
		t.Fatalf("status line should show the pacman attempt:\n%s", plain)
	}
}

func TestInstallBothMissShowsRepoNotice(t *testing.T) {
	m := testModel(80, 24)
	m.installing = "hello-shareware"
	m.installVia = "pacman"

	updated, quit := m.Update(installResultMsg{
		app: "hello-shareware",
		via: "pacman",
		err: fmt.Errorf("couldn't install hello-shareware: not in the omarchy package repo"),
	})
	got := mustModel(t, updated)
	if quit != nil {
		t.Fatal("a failed install must not quit the TUI")
	}
	if got.installing != "" {
		t.Fatalf("installing still set: %q", got.installing)
	}
	plain := ansi.Strip(got.View())
	if !strings.Contains(plain, "Couldn't install hello-shareware") {
		t.Fatalf("status line missing notice:\n%s", plain)
	}
	if !strings.Contains(plain, "not in the omarchy package repo") {
		t.Fatalf("status line missing reason:\n%s", plain)
	}
	if strings.Contains(plain, "pacman install failed") || strings.Contains(plain, "target not found") {
		t.Fatalf("status line leaked pacman internals:\n%s", plain)
	}
}

func TestInstallAuthCanceledDoesNotTryPacman(t *testing.T) {
	m := testModel(80, 24)
	m.installing = "hello-shareware"
	m.installVia = "omarchy"

	updated, cmd := m.Update(installResultMsg{
		app: "hello-shareware",
		via: "omarchy",
		err: fmt.Errorf("couldn't install hello-shareware: authentication canceled"),
	})
	got := mustModel(t, updated)
	if cmd != nil {
		t.Fatal("dismissing the polkit dialog must not start pacman")
	}
	if got.installing != "" {
		t.Fatalf("installing still set: %q", got.installing)
	}
	plain := ansi.Strip(got.View())
	if !strings.Contains(plain, "authentication canceled") {
		t.Fatalf("status line missing cancel notice:\n%s", plain)
	}
}

func TestInstallBusyIgnoresSecondI(t *testing.T) {
	m := testModel(80, 24)
	m.installing = "hello-shareware"
	m.installVia = "omarchy"
	next, cmd := m.handleKey(teaKey("i"))
	got := mustModel(t, next)
	if cmd != nil {
		t.Fatal("a second i while installing must not start another command")
	}
	if got.installing != "hello-shareware" {
		t.Fatalf("installing = %q", got.installing)
	}
}

func TestInstallFromDetailStaysInTUI(t *testing.T) {
	defer stubInstallVia("omarchy")()
	m := testModel(80, 24)
	m.state = stateDetail
	m.detail = &m.apps[1]
	next, cmd := m.handleDetailKey(teaKey("i"))
	got := mustModel(t, next)
	if cmd == nil {
		t.Fatal("detail i should start install")
	}
	if got.action != nil {
		t.Fatal("detail install must not leave the TUI")
	}
	if got.installing != "super-grep-deluxe-professional" {
		t.Fatalf("installing = %q", got.installing)
	}
	if got.state != stateDetail {
		t.Fatal("should stay on the detail view")
	}
}

func TestInstallSuccessStaysInTUI(t *testing.T) {
	m := testModel(80, 24)
	m.installing = "hello-shareware"
	updated, quit := m.Update(installResultMsg{
		app: "hello-shareware",
		ok:  "installed hello-shareware via omarchy",
	})
	if quit != nil {
		t.Fatal("a successful install must not quit the TUI")
	}
	got := mustModel(t, updated)
	plain := ansi.Strip(got.View())
	if !strings.Contains(plain, "Installed hello-shareware via omarchy") {
		t.Fatalf("status line missing success:\n%s", plain)
	}
}

func TestInstallProgressFitsWidth(t *testing.T) {
	m := testModel(40, 24)
	m.installing = "super-grep-deluxe-professional"
	m.installVia = "omarchy"
	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got > 40 {
			t.Errorf("line %d is %d cells: %q", i, got, ansi.Strip(line))
		}
	}
	if got := strings.Count(m.View(), "\n") + 1; got != 24 {
		t.Errorf("view is %d lines, want 24", got)
	}
}

func TestBuyNoticeFitsWidth(t *testing.T) {
	m := testModel(40, 24)
	m.cursor = 1
	m.notice = "couldn't buy omarket: this listing isn't accepting payments right now"
	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got > 40 {
			t.Errorf("line %d is %d cells: %q", i, got, ansi.Strip(line))
		}
	}
	if got := strings.Count(m.View(), "\n") + 1; got != 24 {
		t.Errorf("view is %d lines, want 24", got)
	}
}

func TestCatalogEmptyShowsEmptyState(t *testing.T) {
	m := newModel("https://omarket.dev")
	m.width, m.height = 80, 24
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "loading catalog...") {
		t.Fatalf("unloaded view should say loading:\n%s", plain)
	}
	if strings.Contains(plain, "no apps yet") {
		t.Fatalf("unloaded view should not say empty:\n%s", plain)
	}
	if strings.Contains(plain, "DESCRIPTION") {
		t.Fatalf("loading view should not show table headers:\n%s", plain)
	}

	updated, quit := m.Update(catalogMsg{apps: []client.App{}})
	if quit != nil {
		t.Fatal("empty catalog must not quit")
	}
	got := mustModel(t, updated)
	if !got.loaded {
		t.Fatal("successful empty fetch should mark the catalog loaded")
	}
	plain = ansi.Strip(got.View())
	if strings.Contains(plain, "loading catalog...") {
		t.Fatalf("empty catalog still looks like loading:\n%s", plain)
	}
	for _, want := range []string{
		"no apps yet",
		"omarket sell init",
		"omarket sell claim my-app",
		"omarket sell push",
		"examples/",
		"C, Go, Rust, Ruby",
		"0 apps",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("empty catalog missing %q:\n%s", want, plain)
		}
	}
	for _, header := range []string{"NAME", "WARE", "PRICE", "DESCRIPTION"} {
		if strings.Contains(plain, header) {
			t.Fatalf("empty catalog should not show table headers, found %q:\n%s", header, plain)
		}
	}
	if got := strings.Count(got.View(), "\n") + 1; got != 24 {
		t.Errorf("empty view is %d lines, want 24", got)
	}

	// JSON null / omitted apps is the same as [].
	updated, _ = m.Update(catalogMsg{apps: nil})
	got = mustModel(t, updated)
	plain = ansi.Strip(got.View())
	if strings.Contains(plain, "loading catalog...") || !strings.Contains(plain, "no apps yet") {
		t.Fatalf("nil apps after a successful fetch should look empty:\n%s", plain)
	}
}

func TestEmptyCatalogFitsWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120} {
		for _, h := range []int{10, 24} {
			m := newModel("https://omarket.dev")
			m.width, m.height = w, h
			updated, _ := m.Update(catalogMsg{apps: []client.App{}})
			got := mustModel(t, updated)
			view := got.View()
			for i, line := range strings.Split(view, "\n") {
				if n := lipgloss.Width(line); n > w {
					t.Errorf("%dx%d: line %d is %d cells: %q", w, h, i, n, ansi.Strip(line))
				}
			}
			if n := strings.Count(view, "\n") + 1; n != h {
				t.Errorf("%dx%d: view is %d lines, want %d", w, h, n, h)
			}
		}
	}
}

func TestWrapWords(t *testing.T) {
	got := wrapWords("Be the first. Publish shareware with omarket sell.", 20)
	if len(got) < 2 {
		t.Fatalf("expected a wrap, got %q", got)
	}
	for _, line := range got {
		if lipgloss.Width(line) > 20 {
			t.Errorf("line %q wider than 20", line)
		}
	}
	if join := strings.Join(got, " "); !strings.Contains(join, "omarket sell") {
		t.Fatalf("wrap dropped words: %q", got)
	}
}

func TestColorKeysPreservesWidth(t *testing.T) {
	plain := "  with omarket sell, then copy a license check from examples/."
	got := colorKeys(plain, mutedStyle, []colorKey{
		{"omarket sell", checkoutStyle},
		{"examples/", successStyle},
	})
	if lipgloss.Width(got) != lipgloss.Width(plain) {
		t.Fatalf("styled width %d, plain width %d", lipgloss.Width(got), lipgloss.Width(plain))
	}
	if !strings.Contains(ansi.Strip(got), "omarket sell") || !strings.Contains(ansi.Strip(got), "examples/") {
		t.Fatalf("colorKeys dropped a keyword:\n%s", ansi.Strip(got))
	}
}

func TestCatalogLoadErrorStaysInChrome(t *testing.T) {
	m := testModel(80, 24)
	m.apps = nil
	m.applyFilter()
	updated, quit := m.Update(catalogMsg{err: errCatalogDown})
	if quit != nil {
		t.Fatal("catalog fetch failure must not quit")
	}
	got := updated.(model)
	plain := ansi.Strip(got.View())
	if !strings.Contains(plain, "Couldn't load the catalog") {
		t.Fatalf("missing load error:\n%s", plain)
	}
	if strings.HasPrefix(strings.TrimSpace(plain), "error:") {
		t.Fatalf("full-screen crash view:\n%s", plain)
	}
}

func teaKey(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func mustModel(t *testing.T, v tea.Model) model {
	t.Helper()
	got, ok := v.(model)
	if !ok {
		t.Fatalf("got %T, want model", v)
	}
	return got
}

func stubInstallVia(via string) func() {
	prev := tuiInstallVia
	tuiInstallVia = func(client.CommandRunner) string { return via }
	return func() { tuiInstallVia = prev }
}

var errCatalogDown = errString("connection refused")

type errString string

func (e errString) Error() string { return string(e) }

func TestFilterMatchesWareAndAuthor(t *testing.T) {
	m := testModel(80, 24)
	for query, want := range map[string]int{
		"beerware": 1, "ada": 1, "ware": 3, "postcard": 1, "nomatch": 0,
	} {
		m.filterQuery = query
		m.applyFilter()
		if len(m.filtered) != want {
			t.Errorf("filter %q matched %d apps, want %d", query, len(m.filtered), want)
		}
	}
}
