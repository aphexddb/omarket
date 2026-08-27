package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/aphexddb/omarket/client"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
// case-insensitive substring match against name, description, ware and
// author, so "beerware" or a seller's name narrow the list too).
func (m *model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filterQuery))
	m.filtered = m.filtered[:0]
	for i, a := range m.apps {
		if q == "" ||
			strings.Contains(strings.ToLower(a.Name), q) ||
			strings.Contains(strings.ToLower(a.Description), q) ||
			strings.Contains(strings.ToLower(client.WareOrDefault(a.Ware)), q) ||
			strings.Contains(strings.ToLower(a.Author), q) {
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
	// docStyle margins (2) + title (1) + filter line (1) + column header (1)
	// + help (1) reserve 6 lines; every remaining line is a catalog row.
	rows := m.height - 6
	if rows < 1 {
		rows = 1
	}
	return rows
}

// contentWidth is the usable interior width after docStyle's side margins.
func (m model) contentWidth() int {
	if m.width == 0 {
		return 76 // no WindowSizeMsg yet
	}
	w := m.width - 4
	if w < 24 {
		w = 24
	}
	return w
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

// padCell right-pads plain text s to w display cells, truncating with an
// ellipsis if it is too wide. Cells are padded before styling so ANSI codes
// never skew the column math.
func padCell(s string, w int) string {
	s = truncCell(s, w)
	if n := w - lipgloss.Width(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// truncCell shortens plain text s to at most w display cells, appending an
// ellipsis when it cuts.
func truncCell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return strings.TrimRight(string(r), " ") + "…"
}

// splitLine lays left and right on one line of width w, right-aligned right.
// Both are plain text; the caller styles the result's halves beforehand only
// if their width is unaffected (no padding assumptions broken).
func splitLine(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// serverHost is the display form of the server URL (scheme stripped).
func serverHost(server string) string {
	s := strings.TrimPrefix(server, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.TrimRight(s, "/")
}

// list column geometry, computed against the whole catalog (not the filtered
// subset) so columns don't shift while a filter is typed.
type listCols struct {
	name, ware, price, desc int
}

const colGap = 2

func (m model) columns(w int) listCols {
	// price floor is the "PRICE" header's own width, so columns never skew.
	c := listCols{name: 12, ware: 9, price: 5}
	for _, a := range m.apps {
		if n := lipgloss.Width(a.Name); n > c.name {
			c.name = n
		}
		if n := lipgloss.Width(client.WareOrDefault(a.Ware)); n > c.ware {
			c.ware = n
		}
		if n := lipgloss.Width(priceString(a)); n > c.price {
			c.price = n
		}
	}
	if c.name > 24 {
		c.name = 24
	}
	if c.ware > 14 {
		c.ware = 14
	}
	// cursor(2) + name + owned(2) + ware + price + desc, gap between columns.
	// On narrow terminals give up name width first, then ware width, so at
	// least a sliver of description survives.
	const minDesc = 8
	avail := w - 2 - 2 - c.price - 3*colGap
	if c.name+c.ware+minDesc > avail {
		c.name = max(12, avail-c.ware-minDesc)
	}
	if c.name+c.ware+minDesc > avail {
		c.ware = max(6, avail-c.name-minDesc)
	}
	c.desc = max(0, avail-c.name-c.ware)
	return c
}

func (m model) renderList() string {
	w := m.contentWidth()
	cols := m.columns(w)
	gap := strings.Repeat(" ", colGap)
	var b strings.Builder

	// Title bar: name left; app count and server right.
	right := fmt.Sprintf("%d/%d apps · %s", len(m.filtered), len(m.apps), serverHost(m.server))
	if len(m.apps) == 0 {
		right = serverHost(m.server)
	}
	b.WriteString(splitLine(titleStyle.Render("omarket"), mutedStyle.Render(right), w))
	b.WriteString("\n")

	// Filter line (kept even when blank so the list doesn't jump).
	switch {
	case m.filtering:
		// Keep the tail of a long query visible — that's where typing happens.
		q := []rune(m.filterQuery)
		for len(q) > 0 && lipgloss.Width(string(q))+3 > w {
			q = q[1:]
		}
		fmt.Fprintf(&b, "%s %s%s\n", labelStyle.Render("/"), string(q), "█")
	case m.filterQuery != "":
		fmt.Fprintf(&b, "%s %s %s\n", labelStyle.Render("/"),
			truncCell(m.filterQuery, max(1, w-15)), mutedStyle.Render("(esc clears)"))
	default:
		b.WriteString("\n")
	}

	// Column headers. NAME spans the name column plus the 2-cell owned marker.
	header := padCell("NAME", cols.name+2) + gap + padCell("WARE", cols.ware) +
		gap + padLeft("PRICE", cols.price) + gap + "DESCRIPTION"
	b.WriteString("  " + mutedStyle.Render(truncCell(header, w-2)) + "\n")

	linesUsed := 0
	if len(m.apps) == 0 {
		b.WriteString(mutedStyle.Render("  loading catalog...") + "\n")
		linesUsed++
	} else if len(m.filtered) == 0 {
		b.WriteString(mutedStyle.Render("  no apps match") + "\n")
		linesUsed++
	}

	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	for row := m.offset; row < end; row++ {
		a := m.apps[m.filtered[row]]
		selected := row == m.cursor

		cursor := "  "
		name := padCell(a.Name, cols.name)
		owned := "  "
		if client.HasLicense(a.ID) {
			owned = successStyle.Render("✓") + " "
		}
		ware := padCell(client.WareOrDefault(a.Ware), cols.ware)
		price := padLeft(priceString(a), cols.price)
		desc := truncCell(a.Description, cols.desc)

		nameStyle, descStyle := fgStyle, mutedStyle
		if selected {
			cursor = lipgloss.NewStyle().Foreground(colorAccent).Render("> ")
			nameStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
			descStyle = fgStyle
		}
		priceStyled := fgStyle.Render(price)
		if a.Free() {
			priceStyled = successStyle.Render(price)
		}

		line := cursor + nameStyle.Render(name) + gap + owned +
			wareNameStyle.Render(ware) + gap + priceStyled + gap +
			descStyle.Render(desc)
		// Belt and braces: shrunk columns should already fit, but a row must
		// never wrap — that would break every offset below it.
		b.WriteString(ansi.Truncate(line, w, "…") + "\n")
	}
	// Pad short lists so the help line sits at the bottom of the screen.
	for linesUsed += end - m.offset; linesUsed < visible; linesUsed++ {
		b.WriteString("\n")
	}

	help := "↑/k ↓/j move · enter detail · i install · b buy · / filter · q quit"
	pos := ""
	if len(m.filtered) > 0 {
		pos = fmt.Sprintf("%d/%d · ✓ owned", m.cursor+1, len(m.filtered))
	}
	b.WriteString(helpStyle.Render(truncCell(splitLine(help, pos, w), w)))
	return b.String()
}

// padLeft left-pads plain text s to w display cells (right-aligns it).
func padLeft(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func (m model) renderDetail() string {
	a := m.detail
	w := m.contentWidth()
	var b strings.Builder

	// Title: name, version, owned badge. The name yields to the fixed-width
	// trailers so the line never wraps.
	version, owned := "", ""
	if a.Version != "" {
		version = "v" + strings.TrimPrefix(a.Version, "v")
	}
	if client.HasLicense(a.ID) {
		owned = "OWNED"
	}
	nameW := w
	if version != "" {
		nameW -= lipgloss.Width(version) + 1
	}
	if owned != "" {
		nameW -= lipgloss.Width(owned) + 3 // badge pads 1 cell each side
	}
	title := titleStyle.Render(truncCell(a.Name, nameW))
	if version != "" {
		title += " " + mutedStyle.Render(version)
	}
	if owned != "" {
		title += " " + ownedBadge.Render(owned)
	}
	b.WriteString(title + "\n")

	// Byline: ware tradition, author, price.
	byline := wareNameStyle.Render(client.WareOrDefault(a.Ware))
	if a.Author != "" {
		byline += mutedStyle.Render(" · by ") + fgStyle.Render(truncCell(a.Author, 40))
	}
	if a.Free() {
		byline += mutedStyle.Render(" · ") + successStyle.Render("FREE")
	} else {
		byline += mutedStyle.Render(" · ") + fgStyle.Render(priceString(*a))
	}
	b.WriteString(byline + "\n\n")

	b.WriteString(lipgloss.NewStyle().Foreground(colorFg).Width(w).Render(a.Description))
	b.WriteString("\n\n")

	// The ware ask, quoted in the author's voice.
	if a.Comment != "" {
		quote := lipgloss.NewStyle().
			Foreground(colorAccent).
			Width(w - 2).
			Render(a.Comment)
		b.WriteString(lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(colorAccent).
			PaddingLeft(1).
			Render(quote))
		b.WriteString("\n\n")
	}

	row := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%s %s\n", labelStyle.Render(padCell(label, 9)), truncCell(value, w-10))
	}
	row("source", a.Source)
	row("homepage", a.Homepage)
	row("package", a.Pkgname)
	if len(a.Tags) > 0 {
		row("tags", strings.Join(a.Tags, ", "))
	}

	b.WriteString("\n")
	help := "i install · esc back · q quit"
	if !a.Free() && !client.HasLicense(a.ID) {
		help = "i install · b buy · esc back · q quit"
	}
	b.WriteString(helpStyle.Render(truncCell(help, w)))
	return b.String()
}
