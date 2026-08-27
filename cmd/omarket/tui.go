package main

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aphexddb/omarket/client"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// runTUI launches the full-screen catalog browser. Install stays in the
// TUI and uses Omarchy's polkit dialog for sudo. Buy is started inside
// the TUI so a failed checkout stays on the status line; only a live
// Checkout URL leaves alt-screen, for the QR + poll.
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
	if fm.action == nil {
		return nil
	}

	switch fm.action.kind {
	case "buy":
		return runCheckout(fm.server, fm.action.app, fm.action.res)
	}
	return nil
}

type viewState int

const (
	stateList viewState = iota
	stateDetail
)

type tuiAction struct {
	kind string // "buy" leaves the TUI for QR checkout
	app  string
	res  client.BuyResult
}

type catalogMsg struct {
	apps  []client.App
	stale bool
	err   error
}

type buyResultMsg struct {
	app string
	res client.BuyResult
	err error
}

type installResultMsg struct {
	app     string
	pkg     string
	via     string
	ok      string
	err     error
	tryNext string // "pacman" after an omarchy miss
}

// tuiInstallVia names the first install helper for the status line.
// Tests stub this so they don't depend on the host PATH.
var tuiInstallVia = client.InstallVia

func fetchCatalogCmd(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		apps, stale, err := c.GetCatalogCached(context.Background())
		return catalogMsg{apps: apps, stale: stale, err: err}
	}
}

// reconcileCmd resolves any pending purchase records in the background —
// the same hook `runLicenses` runs at startup, minus the
// printed notices: the TUI's alt-screen has nowhere sane to put them
// mid-render. A resolved purchase's license still lands on disk either way;
// the catalog view picks up the new "owned" mark next launch. Errors are
// swallowed for the same reason runLicenses swallows them: best-effort.
func reconcileCmd() tea.Cmd {
	return func() tea.Msg {
		if pub, err := resolvePublicKey(); err == nil {
			_, _ = client.Reconcile(context.Background(), pub)
		}
		return nil
	}
}

func startBuyCmd(server, appID string) tea.Cmd {
	return func() tea.Msg {
		res, err := client.NewClient(server).Buy(context.Background(), client.BuyRequest{App: appID})
		if err != nil {
			return buyResultMsg{app: appID, err: buyStartError(appID, err)}
		}
		return buyResultMsg{app: appID, res: res}
	}
}

func startInstallCmd(appID, pkgname, via string) tea.Cmd {
	return func() tea.Msg {
		msg, err := client.InstallOnce(nil, pkgname, via)
		if err != nil {
			if via == "omarchy" && client.IsMissingPackage(err) && client.HasHelper(nil, "pacman") {
				return installResultMsg{app: appID, pkg: pkgname, via: via, tryNext: "pacman"}
			}
			return installResultMsg{app: appID, pkg: pkgname, via: via, err: err}
		}
		return installResultMsg{app: appID, pkg: pkgname, via: via, ok: msg}
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

	// loaded is true after a successful catalog fetch, including an empty
	// one. Distinguishes "still waiting" from "the catalog really has no apps".
	loaded bool
	// loadErr is a failed catalog fetch. It is shown in the list body, not
	// as a process-killing crash; r retries.
	loadErr error
	// notice is a recoverable status-line message (buy/install failed, etc).
	// esc dismisses it. The catalog stays on screen.
	notice   string
	noticeOK bool // true = success (green), false = error (red)
	// buying / installing are in-flight app ids. Empty = idle.
	buying     string
	installing string
	installVia string
	installPkg string

	action *tuiAction
	stale  bool // catalog served from the offline disk cache

	width, height int
}

func newModel(server string) model {
	return model{server: server}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCatalogCmd(client.NewClient(m.server)), reconcileCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case catalogMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			m.notice = "Couldn't load the catalog"
			return m, nil
		}
		m.loadErr = nil
		m.loaded = true
		m.apps = msg.apps
		if m.apps == nil {
			m.apps = []client.App{}
		}
		m.stale = msg.stale
		m.applyFilter()
		return m, nil

	case buyResultMsg:
		m.buying = ""
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.noticeOK = false
			return m, nil
		}
		m.action = &tuiAction{
			kind: "buy",
			app:  msg.app,
			res:  msg.res,
		}
		return m, tea.Quit

	case installResultMsg:
		if msg.tryNext != "" {
			m.installing = msg.app
			if msg.pkg != "" {
				m.installPkg = msg.pkg
			}
			m.installVia = msg.tryNext
			m.notice = ""
			m.noticeOK = false
			pkg := m.installPkg
			if pkg == "" {
				pkg = msg.app
			}
			return m, startInstallCmd(msg.app, pkg, msg.tryNext)
		}
		m.installing = ""
		m.installVia = ""
		m.installPkg = ""
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.noticeOK = false
			return m, nil
		}
		m.notice = msg.ok
		m.noticeOK = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) busy() bool { return m.buying != "" || m.installing != "" }

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.busy() {
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}
	if m.notice != "" && msg.String() == "esc" {
		m.notice = ""
		m.noticeOK = false
		return m, nil
	}
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
	case "r":
		if m.loadErr != nil {
			m.notice = ""
			m.loadErr = nil
			return *m, fetchCatalogCmd(client.NewClient(m.server))
		}
	case "i":
		return m.beginInstall()
	case "b":
		return m.beginBuy()
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
		return m.beginInstall()
	case "b":
		return m.beginBuy()
	}
	return *m, nil
}

func (m *model) selectedApp() (*client.App, bool) {
	if m.state == stateDetail && m.detail != nil {
		return m.detail, true
	}
	return m.selected()
}

func (m *model) beginBuy() (tea.Model, tea.Cmd) {
	a, ok := m.selectedApp()
	if !ok || a.Free() {
		return *m, nil
	}
	m.notice = ""
	m.noticeOK = false
	m.buying = a.ID
	return *m, startBuyCmd(m.server, a.ID)
}

func (m *model) beginInstall() (tea.Model, tea.Cmd) {
	a, ok := m.selectedApp()
	if !ok {
		return *m, nil
	}
	pkg := a.Pkgname
	if pkg == "" {
		pkg = a.ID
	}
	m.notice = ""
	m.noticeOK = false
	m.installing = a.ID
	m.installPkg = pkg
	m.installVia = tuiInstallVia(nil)
	if m.installVia == "" || m.installVia == "package manager" {
		m.installVia = "omarchy"
	}
	return *m, startInstallCmd(a.ID, pkg, m.installVia)
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
	if m.state == stateDetail && m.detail != nil {
		return docStyle.Render(m.renderDetail())
	}
	return docStyle.Render(m.renderList())
}

// formatNotice turns a buyer-facing error into a status-line sentence:
// capitalize, and prefer an em dash over the first colon.
func formatNotice(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if i := strings.Index(s, ": "); i > 0 {
		s = s[:i] + " — " + s[i+2:]
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	upper := unicode.ToUpper(r)
	if upper == r {
		return s
	}
	return string(upper) + s[size:]
}

func (m model) renderFooter(w int, idleHelp, idleRight string) string {
	switch {
	case m.installing != "":
		via := m.installVia
		if via == "" {
			via = "omarchy"
		}
		left := mutedStyle.Render(truncCell("Installing "+m.installing+" via "+via+"…", max(1, w-6)))
		return truncCell(splitLine(left, helpStyle.Render("wait"), w), w)
	case m.buying != "":
		left := mutedStyle.Render(truncCell("Starting checkout for "+m.buying+"…", max(1, w-6)))
		return truncCell(splitLine(left, helpStyle.Render("wait"), w), w)
	case m.notice != "":
		msg := truncCell(formatNotice(m.notice), max(1, w-4))
		style := errorStyle
		if m.noticeOK {
			style = successStyle
		}
		left := style.Render(msg)
		return truncCell(splitLine(left, helpStyle.Render("esc"), w), w)
	default:
		return helpStyle.Render(truncCell(splitLine(idleHelp, idleRight, w), w))
	}
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
		if m.loaded {
			right = fmt.Sprintf("0 apps · %s", serverHost(m.server))
		} else {
			right = serverHost(m.server)
		}
	}
	if m.stale {
		right += " (cached)"
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

	// Column headers belong to a table. An empty, loading, or failed
	// catalog is a status, not a zero-row table — "catalog empty" under
	// NAME looks like an app named that.
	showTable := len(m.apps) > 0
	if showTable {
		header := padCell("NAME", cols.name+2) + gap + padCell("WARE", cols.ware) +
			gap + padLeft("PRICE", cols.price) + gap + "DESCRIPTION"
		b.WriteString("  " + mutedStyle.Render(truncCell(header, w-2)) + "\n")
	}

	linesUsed := 0
	if m.loadErr != nil && len(m.apps) == 0 {
		b.WriteString(errorStyle.Render(truncCell("  Couldn't load the catalog. r retries.", w)) + "\n")
		linesUsed++
	} else if len(m.apps) == 0 && !m.loaded {
		b.WriteString(mutedStyle.Render("  loading catalog...") + "\n")
		linesUsed++
	} else if len(m.apps) == 0 {
		b.WriteString(mutedStyle.Render("  catalog empty") + "\n")
		linesUsed++
	} else if len(m.filtered) == 0 {
		b.WriteString(mutedStyle.Render("  no apps match") + "\n")
		linesUsed++
	}

	visible := m.visibleRows()
	if !showTable {
		// Header line was skipped; keep the view the same height.
		visible++
	}
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
	if m.loadErr != nil && len(m.apps) == 0 {
		help = "r retry · q quit"
	} else if m.loaded && len(m.apps) == 0 {
		help = "q quit"
	}
	pos := ""
	if len(m.filtered) > 0 {
		pos = fmt.Sprintf("%d/%d · ✓ owned", m.cursor+1, len(m.filtered))
	}
	b.WriteString(m.renderFooter(w, help, pos))
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
	b.WriteString(m.renderFooter(w, help, ""))
	return b.String()
}
