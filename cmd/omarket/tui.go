package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/aphexddb/omarchy-shareware/client"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// runTUI launches the full-screen catalog browser. If the user requests an
// install or buy from within the TUI, it exits cleanly first and the action
// is then carried out via the plain-terminal subcommands (buy needs a QR
// code and live progress, install may need sudo's prompt).
func runTUI() error {
	server := client.ResolveServer("")
	m := newModel(server)

	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}

	fm, ok := final.(model)
	if !ok {
		return nil
	}
	if fm.err != nil {
		return fm.err
	}
	if fm.action == nil {
		return nil
	}

	switch fm.action.kind {
	case "install":
		return runInstall([]string{fm.action.app})
	case "buy":
		return runBuy([]string{fm.action.app})
	}
	return nil
}

type viewState int

const (
	stateList viewState = iota
	stateDetail
)

type tuiAction struct {
	kind string // "install" or "buy"
	app  string
}

type catalogMsg struct {
	apps []client.App
	err  error
}

func fetchCatalogCmd(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		apps, err := c.GetCatalog(context.Background())
		return catalogMsg{apps: apps, err: err}
	}
}

// model is a hand-rolled catalog browser: bubbles/list pulls in a fuzzy
// matcher not present in go.sum, so filtering/scrolling/selection are
// implemented directly here instead.
type model struct {
	server string

	apps     []client.App
	filtered []int // indices into apps currently shown

	cursor int
	offset int // first visible row, for scrolling

	filtering   bool
	filterQuery string

	state  viewState
	detail *client.App

	err    error
	action *tuiAction

	width, height int
}

func newModel(server string) model {
	return model{server: server}
}

func (m model) Init() tea.Cmd {
	return fetchCatalogCmd(client.NewClient(m.server))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case catalogMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.apps = msg.apps
		m.applyFilter()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	switch m.state {
	case stateDetail:
		return m.handleDetailKey(msg)
	default:
		return m.handleListKey(msg)
	}
}

func (m model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.filtering = false
		m.filterQuery = ""
		m.applyFilter()
	case tea.KeyEnter:
		m.filtering = false
	case tea.KeyBackspace:
		if len(m.filterQuery) > 0 {
			r := []rune(m.filterQuery)
			m.filterQuery = string(r[:len(r)-1])
			m.applyFilter()
		}
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyRunes:
		m.filterQuery += string(msg.Runes)
		m.applyFilter()
	}
	return m, nil
}

func (m *model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return *m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "/":
		m.filtering = true
	case "esc":
		if m.filterQuery != "" {
			m.filterQuery = ""
			m.applyFilter()
		}
	case "enter":
		if a, ok := m.selected(); ok {
			m.detail = a
			m.state = stateDetail
		}
	case "i":
		if a, ok := m.selected(); ok {
			m.action = &tuiAction{kind: "install", app: a.ID}
			return *m, tea.Quit
		}
	case "b":
		if a, ok := m.selected(); ok && !a.Free() {
			m.action = &tuiAction{kind: "buy", app: a.ID}
			return *m, tea.Quit
		}
	}
	return *m, nil
}

func (m *model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.state = stateList
		m.detail = nil
	case "ctrl+c":
		return *m, tea.Quit
	case "i":
		m.action = &tuiAction{kind: "install", app: m.detail.ID}
		return *m, tea.Quit
	case "b":
		if !m.detail.Free() {
			m.action = &tuiAction{kind: "buy", app: m.detail.ID}
			return *m, tea.Quit
		}
	}
	return *m, nil
}

// applyFilter recomputes m.filtered from m.apps and m.filterQuery (a
// case-insensitive substring match against name and description).
func (m *model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filterQuery))
	m.filtered = m.filtered[:0]
	for i, a := range m.apps {
		if q == "" ||
			strings.Contains(strings.ToLower(a.Name), q) ||
			strings.Contains(strings.ToLower(a.Description), q) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	m.offset = 0
}

func (m *model) moveCursor(delta int) {
	if len(m.filtered) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	visible := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
}

func (m model) visibleRows() int {
	// header (title + blank) + footer (blank + help) reserve ~4 lines.
	rows := m.height - 4
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m model) selected() (*client.App, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil, false
	}
	a := m.apps[m.filtered[m.cursor]]
	return &a, true
}

func (m model) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("error: %v", m.err)) + "\n\n" + helpStyle.Render("q to quit")
	}
	if m.state == stateDetail && m.detail != nil {
		return docStyle.Render(m.renderDetail())
	}
	return docStyle.Render(m.renderList())
}

func (m model) renderList() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("omarket"))
	b.WriteString("\n")
	if m.filtering {
		fmt.Fprintf(&b, "%s %s%s\n", labelStyle.Render("filter:"), m.filterQuery, "█")
	} else if m.filterQuery != "" {
		fmt.Fprintf(&b, "%s %q (esc to clear)\n", labelStyle.Render("filter:"), m.filterQuery)
	} else {
		b.WriteString("\n")
	}

	if len(m.apps) == 0 {
		b.WriteString("\n" + mutedStyle.Render("loading catalog...") + "\n")
	} else if len(m.filtered) == 0 {
		b.WriteString("\n" + mutedStyle.Render("no apps match") + "\n")
	}

	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	for row := m.offset; row < end; row++ {
		a := m.apps[m.filtered[row]]
		cursor := "  "
		line := fmt.Sprintf("%s  %s", priceString(a), a.Description)
		name := a.Name
		if client.HasLicense(a.ID) {
			name += " " + ownedBadge.Render("OWNED")
		}
		if row == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(colorAccent).Render("> ")
			name = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(a.Name)
			if client.HasLicense(a.ID) {
				name += " " + ownedBadge.Render("OWNED")
			}
		}
		fmt.Fprintf(&b, "%s%s\n    %s\n", cursor, name, mutedStyle.Render(line))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/k up  ↓/j down  enter detail  i install  b buy  / filter  q quit"))
	return b.String()
}

func (m model) renderDetail() string {
	a := m.detail
	var b strings.Builder

	b.WriteString(titleStyle.Render(a.Name))
	if client.HasLicense(a.ID) {
		b.WriteString(" " + ownedBadge.Render("OWNED"))
	}
	b.WriteString("\n\n")
	b.WriteString(fgStyle.Render(a.Description))
	b.WriteString("\n\n")

	row := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%s %s\n", labelStyle.Render(label+":"), value)
	}
	row("version", a.Version)
	row("price", priceString(*a))
	row("kind", a.Kind)
	row("source", a.Source)
	row("homepage", a.Homepage)
	row("package", a.Pkgname)

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("i install  ·  b buy  ·  esc back  ·  q quit"))
	return b.String()
}
