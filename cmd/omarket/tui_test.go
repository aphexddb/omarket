package main

import (
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
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
