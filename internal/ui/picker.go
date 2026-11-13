// Package ui has spin-ssh's terminal UI, built on Bubble Tea: an fzf-style picker, a
// spinner for slow API calls and a one-line prompt.
package ui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"
)

// Version is shown on every screen; main sets it. UpdateNotice, when set, is shown
// next to it (such as "v0.2.0 available: spin-ssh update").
var Version, UpdateNotice string

// Item is one row in the picker.
type Item struct {
	// Key identifies the item across refreshes, so the cursor stays on it.
	Key string
	// Columns are shown aligned under PickerOptions.Headers, and searched.
	Columns []string
	// Badge is a short note at the end of the row, such as "2h ago". Not searched.
	Badge string
	// Title and Fields fill the details panel for the selected row.
	Title  string
	Fields []Field
	Value  any
}

// Field is one "label  value" line in the details panel.
type Field struct {
	Label string
	Value string
	Tone  Tone
}

// Refreshed is what a background refresh returns: new items and a status line.
type Refreshed struct {
	Items  []Item
	Status string
	Err    error
}

// PickerOptions configures Pick.
type PickerOptions struct {
	// Title is shown on the search box, Prompt in front of the query.
	Title       string
	Placeholder string
	// Noun names the items in the list header ("site" shows "Sites", "3 of 35").
	Noun        string
	Headers     []string
	BadgeHeader string
	// Action is what enter does, for the key help ("connect").
	Action string
	Query  string
	Items  []Item
	Status string
	// Refresh reloads the items in the background: on ctrl+r, and when the picker
	// opens if AutoRefresh is set.
	Refresh     func() Refreshed
	AutoRefresh bool
}

// Pick shows the picker and returns the chosen item, or nil if the user quit.
func Pick(opts PickerOptions) (*Item, error) {
	m := newPicker(opts)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	return final.(*picker).chosen, nil
}

type match struct {
	index int
	hits  []int // byte offsets into the item's joined columns
}

type picker struct {
	opts    PickerOptions
	th      theme
	input   textinput.Model
	spin    spinner.Model
	items   []Item
	matches []match
	cursor  int
	offset  int
	width   int
	height  int
	loading bool
	status  string
	failed  bool
	chosen  *Item
}

func newPicker(opts PickerOptions) *picker {
	t := currentTheme()
	if opts.Noun == "" {
		opts.Noun = "item"
	}
	in := textinput.New()
	in.Prompt = "❯ "
	in.Placeholder = opts.Placeholder
	styles := textinput.DefaultStyles(true)
	styles.Focused.Prompt = t.fg(t.accent).Bold(true)
	styles.Focused.Text = t.fg(t.text)
	styles.Focused.Placeholder = t.fg(t.faint)
	styles.Cursor.Color = t.accent
	in.SetStyles(styles)
	in.SetValue(opts.Query)

	m := &picker{
		opts:    opts,
		th:      t,
		input:   in,
		spin:    spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(t.fg(t.accent))),
		items:   opts.Items,
		status:  opts.Status,
		loading: opts.AutoRefresh && opts.Refresh != nil,
	}
	m.filter()
	return m
}

func (m *picker) refreshCmd() tea.Cmd {
	refresh := m.opts.Refresh
	return tea.Batch(m.spin.Tick, func() tea.Msg { return refresh() })
}

func (m *picker) Init() tea.Cmd {
	cmds := []tea.Cmd{m.input.Focus()}
	if m.loading {
		cmds = append(cmds, m.refreshCmd())
	}
	return tea.Batch(cmds...)
}

func (m *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, m.width-8))
		return m, nil

	case Refreshed:
		m.loading = false
		if msg.Err != nil {
			m.status, m.failed = "refresh failed: "+msg.Err.Error(), true
			return m, nil
		}
		key := ""
		if it := m.selected(); it != nil {
			key = it.Key
		}
		m.items, m.status, m.failed = msg.Items, msg.Status, false
		m.filter()
		for i, mt := range m.matches {
			if m.items[mt.index].Key == key {
				m.cursor = i
			}
		}
		return m, nil

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.input.Value() == "" {
				return m, tea.Quit
			}
			m.input.SetValue("")
			m.filter()
			m.cursor, m.offset = 0, 0
			return m, nil
		case "enter":
			if it := m.selected(); it != nil {
				m.chosen = it
				return m, tea.Quit
			}
			return m, nil
		case "ctrl+r":
			if m.opts.Refresh == nil || m.loading {
				return m, nil
			}
			m.loading = true
			return m, m.refreshCmd()
		case "up", "ctrl+p", "ctrl+k":
			m.move(-1)
			return m, nil
		case "down", "ctrl+n", "ctrl+j", "tab":
			m.move(1)
			return m, nil
		case "shift+tab":
			m.move(-1)
			return m, nil
		case "pgup":
			m.move(-m.layout().rows)
			return m, nil
		case "pgdown":
			m.move(m.layout().rows)
			return m, nil
		}
	}

	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.filter()
		m.cursor, m.offset = 0, 0
	}
	return m, cmd
}

func (m *picker) move(n int) {
	m.cursor = max(0, min(len(m.matches)-1, m.cursor+n))
}

func (m *picker) selected() *Item {
	if m.cursor < 0 || m.cursor >= len(m.matches) {
		return nil
	}
	return &m.items[m.matches[m.cursor].index]
}

func (m *picker) filter() {
	m.matches = filterItems(m.items, m.input.Value())
	m.cursor = max(0, min(m.cursor, len(m.matches)-1))
}

// Filter returns the indexes of items matching query, best first. An empty query
// keeps every item in its original order.
func Filter(items []Item, query string) []int {
	ms := filterItems(items, query)
	out := make([]int, len(ms))
	for i, mt := range ms {
		out[i] = mt.index
	}
	return out
}

// filterItems matches like fzf: space-separated terms must all fuzzy-match the item's
// columns. Ties keep the items' order, so callers can put likely picks first.
func filterItems(items []Item, query string) []match {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		all := make([]match, len(items))
		for i := range all {
			all[i] = match{index: i}
		}
		return all
	}
	source := make([]string, len(items))
	for i, it := range items {
		source[i] = strings.Join(it.Columns, " ")
	}
	type acc struct {
		score int
		hits  []int
	}
	found := map[int]acc{}
	for t, term := range terms {
		next := map[int]acc{}
		for _, fm := range fuzzy.Find(term, source) {
			prev, ok := found[fm.Index]
			if ok || t == 0 {
				next[fm.Index] = acc{prev.score + fm.Score, append(prev.hits, fm.MatchedIndexes...)}
			}
		}
		found = next
	}
	out := make([]match, 0, len(found))
	for idx, a := range found {
		out = append(out, match{index: idx, hits: a.hits})
	}
	slices.SortFunc(out, func(a, b match) int {
		return cmp.Or(cmp.Compare(found[b.index].score, found[a.index].score), cmp.Compare(a.index, b.index))
	})
	return out
}

// layout is how the screen is split. The search box is 3 lines and the key help 1.
type layout struct {
	listW, listH     int
	detailW, detailH int
	detailsBeside    bool
	rows             int // visible list rows, under the column headers
}

func (m *picker) layout() layout {
	body := max(4, m.height-4)
	l := layout{listW: m.width, listH: body}
	switch {
	case m.width >= 96:
		l.detailsBeside = true
		l.listW = max(50, m.width*3/5)
		l.detailW, l.detailH = m.width-l.listW-1, body
	case body >= 18:
		l.detailW, l.detailH = m.width, 10
		l.listH = body - l.detailH
	}
	l.rows = max(1, l.listH-3) // borders and header row
	return l
}

func (m *picker) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.WindowTitle = m.opts.Title
	if m.width == 0 || m.height == 0 {
		return v
	}
	t := m.th
	l := m.layout()

	title := t.fg(t.accent).Bold(true).Render(m.opts.Title)
	if Version != "" {
		title += " " + t.fg(t.faint).Render(Version)
	}
	if UpdateNotice != "" {
		title += t.fg(t.faint).Render(" · ") + t.fg(t.warn).Render(UpdateNotice)
	}
	search := box(m.width, 3, title, m.statusText(),
		[]string{" " + m.input.View()}, t.fg(t.accent))

	noun := m.opts.Noun
	count := t.fg(t.dim).Render(strconv.Itoa(len(m.matches)) + " of " + strconv.Itoa(len(m.items)))
	list := box(l.listW, l.listH, t.fg(t.text).Bold(true).Render(strings.ToUpper(noun[:1])+noun[1:]+"s"), count,
		m.listBody(l.listW-2, l.rows), t.fg(t.border))

	body := list
	if l.detailW > 0 {
		details := m.detailsBox(l.detailW, l.detailH)
		if l.detailsBeside {
			body = lipgloss.JoinHorizontal(lipgloss.Top, list, " ", details)
		} else {
			body = list + "\n" + details
		}
	}

	v.SetContent(search + "\n" + body + "\n" + m.help())
	return v
}

func (m *picker) statusText() string {
	t := m.th
	switch {
	case m.loading && len(m.items) == 0:
		return m.spin.View() + t.fg(t.dim).Render(" loading…")
	case m.loading:
		return m.spin.View() + t.fg(t.dim).Render(" refreshing…")
	case m.failed:
		return t.fg(t.bad).Render(m.status)
	default:
		return t.fg(t.dim).Render(m.status)
	}
}

// listBody renders the column headers and the visible rows, each exactly w wide.
func (m *picker) listBody(w, rows int) []string {
	t := m.th
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}

	colW, badgeW := m.columnWidths(w)
	headStyle := t.fg(t.dim).Bold(true)
	head := make([]string, len(colW))
	for c := range colW {
		h := ""
		if c < len(m.opts.Headers) {
			h = strings.ToUpper(m.opts.Headers[c])
		}
		head[c] = pad(truncate(h, colW[c]), colW[c])
	}
	header := "  " + headStyle.Render(strings.Join(head, "  "))
	if badgeW > 0 {
		header = pad(header, w-badgeW-1) + headStyle.Render(pad(strings.ToUpper(m.opts.BadgeHeader), badgeW))
	}
	lines := []string{header}

	if len(m.matches) == 0 {
		msg := "No " + m.opts.Noun + "s match “" + m.input.Value() + "”"
		if len(m.items) == 0 {
			msg = "Nothing here yet"
		}
		lines = append(lines, "", lipgloss.PlaceHorizontal(w, lipgloss.Center, t.fg(t.dim).Render(msg)))
		return lines
	}

	for i := range rows {
		n := m.offset + i
		if n >= len(m.matches) {
			break
		}
		lines = append(lines, m.row(m.matches[n], n == m.cursor, w, colW, badgeW))
	}
	return lines
}

// columnWidths fits the columns into w: every column but the first keeps its natural
// width (up to 24); the first gets the rest.
func (m *picker) columnWidths(w int) ([]int, int) {
	n := len(m.opts.Headers)
	badgeW := lipgloss.Width(m.opts.BadgeHeader)
	for _, it := range m.items {
		n = max(n, len(it.Columns))
		badgeW = max(badgeW, lipgloss.Width(it.Badge))
	}
	natural := make([]int, n)
	for c := range n {
		if c < len(m.opts.Headers) {
			natural[c] = lipgloss.Width(m.opts.Headers[c])
		}
		for _, it := range m.items {
			if c < len(it.Columns) {
				natural[c] = max(natural[c], lipgloss.Width(it.Columns[c]))
			}
		}
	}

	// "▌ " + columns separated by 2 spaces + 2 spaces + badge + 1 space.
	avail := w - 2 - 2*(n-1) - 1
	if badgeW > 0 {
		avail -= badgeW + 2
	}
	colW := make([]int, n)
	rest := avail
	for c := n - 1; c >= 1; c-- {
		colW[c] = min(natural[c], 24, max(6, rest/3))
		rest -= colW[c]
	}
	if n > 0 {
		colW[0] = max(8, min(natural[0], rest))
		// Spare width goes to the first column's padding, so badges sit at the right edge.
	}
	return colW, badgeW
}

func (m *picker) row(mt match, selected bool, w int, colW []int, badgeW int) string {
	t := m.th
	it := m.items[mt.index]
	base := t.fg(t.text)
	hit := t.fg(t.accent).Bold(true).Underline(true)
	bar := base.Render("  ")
	if selected {
		base = base.Background(t.selBg).Bold(true)
		hit = hit.Background(t.selBg)
		bar = t.fg(t.accent).Background(t.selBg).Render("▌ ")
	}

	var b strings.Builder
	b.WriteString(bar)
	offset := 0 // byte offset of this column in the joined search text
	for c, width := range colW {
		col := ""
		if c < len(it.Columns) {
			col = it.Columns[c]
		}
		style := base
		if c > 0 && !selected {
			style = t.fg(t.dim)
		}
		if c > 0 {
			b.WriteString(base.Render("  "))
		}
		b.WriteString(highlight(truncate(col, width), offset, mt.hits, style, hit))
		if gap := width - lipgloss.Width(truncate(col, width)); gap > 0 {
			b.WriteString(base.Render(strings.Repeat(" ", gap)))
		}
		offset += len(col) + 1
	}
	line := b.String()
	if badgeW > 0 {
		badge := t.fg(t.dim)
		if selected {
			badge = badge.Background(t.selBg)
		}
		line += base.Render(strings.Repeat(" ", max(0, w-lipgloss.Width(line)-badgeW-1)))
		line += badge.Render(pad(it.Badge, badgeW))
	}
	return line + base.Render(strings.Repeat(" ", max(0, w-lipgloss.Width(line))))
}

// highlight styles the runes of s whose byte offset (plus start) is in hits.
func highlight(s string, start int, hits []int, base, hit lipgloss.Style) string {
	if len(hits) == 0 {
		return base.Render(s)
	}
	set := make(map[int]bool, len(hits))
	for _, h := range hits {
		set[h-start] = true
	}
	var b, run strings.Builder
	runHit := false
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runHit {
			b.WriteString(hit.Render(run.String()))
		} else {
			b.WriteString(base.Render(run.String()))
		}
		run.Reset()
	}
	for i, r := range s {
		if set[i] != runHit {
			flush()
			runHit = set[i]
		}
		run.WriteRune(r)
	}
	flush()
	return b.String()
}

func (m *picker) detailsBox(w, h int) string {
	t := m.th
	it := m.selected()
	if it == nil {
		return box(w, h, "", "", nil, t.fg(t.border))
	}
	labelW := 0
	for _, f := range it.Fields {
		labelW = max(labelW, lipgloss.Width(f.Label))
	}
	valueW := max(8, w-2-2-labelW-2)
	lines := []string{""}
	for _, f := range it.Fields {
		wrapped := strings.Split(lipgloss.NewStyle().Width(valueW).Render(f.Value), "\n")
		for i, part := range wrapped {
			label := ""
			if i == 0 {
				label = f.Label
			}
			lines = append(lines, "  "+t.fg(t.dim).Render(pad(label, labelW))+"  "+t.tone(f.Tone).Render(part))
		}
	}
	return box(w, h, t.fg(t.accent).Bold(true).Render(it.Title), "", lines, t.fg(t.border))
}

func (m *picker) help() string {
	t := m.th
	key := t.fg(t.text).Bold(true)
	desc := t.fg(t.dim)
	esc := "quit"
	if m.input.Value() != "" {
		esc = "clear"
	}
	parts := []string{key.Render("↑↓") + " " + desc.Render("move"), key.Render("enter") + " " + desc.Render(cmp.Or(m.opts.Action, "select"))}
	if m.opts.Refresh != nil {
		parts = append(parts, key.Render("ctrl+r")+" "+desc.Render("refresh"))
	}
	parts = append(parts, key.Render("esc")+" "+desc.Render(esc))
	return " " + truncate(strings.Join(parts, t.fg(t.faint).Render("  ·  ")), m.width-1)
}
