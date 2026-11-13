package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func testItems() []Item {
	var items []Item
	for _, d := range []string{"shop.example.com", "blog.example.org", "staging.blog.example.org", "docs.example.net"} {
		items = append(items, Item{Key: d, Columns: []string{d, "srv"}, Title: d, Fields: []Field{{Label: "Server", Value: "srv"}}, Value: d})
	}
	return items
}

func newTestPicker(items []Item) *picker {
	m := newPicker(PickerOptions{Title: "spin-ssh", Noun: "site", Headers: []string{"Domain", "Server"}, Items: items})
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	return m
}

func typeText(m *picker, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestFilterTermsMustAllMatch(t *testing.T) {
	items := testItems()
	got := Filter(items, "staging blog")
	if len(got) != 1 || items[got[0]].Key != "staging.blog.example.org" {
		t.Fatalf("Filter = %v", got)
	}
	if got := Filter(items, ""); len(got) != len(items) || got[0] != 0 {
		t.Fatalf("empty query = %v, want all in order", got)
	}
	if got := Filter(items, "zzz"); len(got) != 0 {
		t.Fatalf("no match = %v", got)
	}
}

func TestPickerTypeAndSelect(t *testing.T) {
	m := newTestPicker(testItems())
	typeText(m, "blog")
	if len(m.matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(m.matches))
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	second := m.selected().Key
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chosen == nil || m.chosen.Key != second || cmd == nil {
		t.Fatalf("chosen = %v, want %s", m.chosen, second)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "2 of 4") || !strings.Contains(view, "╭─ "+second) {
		t.Fatalf("view missing count or preview:\n%s", view)
	}
}

func TestPickerEscClearsThenQuits(t *testing.T) {
	m := newTestPicker(testItems())
	typeText(m, "docs")
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); m.input.Value() != "" || len(m.matches) != 4 || cmd != nil {
		t.Fatalf("first esc: query %q, %d matches", m.input.Value(), len(m.matches))
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd == nil || m.chosen != nil {
		t.Fatal("second esc should quit without a choice")
	}
}

func TestHighlightMarksMatchedRunes(t *testing.T) {
	plain, hit := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true)
	got := highlight("neptune", 10, []int{10, 11, 14}, plain, hit)
	want := hit.Render("ne") + plain.Render("pt") + hit.Render("u") + plain.Render("ne")
	if got != want {
		t.Fatalf("highlight = %q, want %q", got, want)
	}
}

func TestPickerRefreshKeepsSelection(t *testing.T) {
	m := newTestPicker(testItems())
	m.loading = true
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	want := m.selected().Key

	// The refresh adds a site at the top; the cursor should follow the same site.
	items := append([]Item{{Key: "new.example.com", Columns: []string{"new.example.com", "srv"}}}, testItems()...)
	m.Update(Refreshed{Items: items, Status: "updated just now"})
	if got := m.selected().Key; got != want || m.loading {
		t.Fatalf("selected %s after refresh, want %s (loading=%v)", got, want, m.loading)
	}
}

func TestPickerFillsTerminalExactly(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 30}, {120, 20}, {200, 50}} {
		m := newTestPicker(testItems())
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		typeText(m, "exa")
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, line := range lines[:len(lines)-1] { // the key help may be shorter
			if w := ansi.StringWidth(line); w != size[0] {
				t.Errorf("%dx%d: line is %d wide: %q", size[0], size[1], w, ansi.Strip(line))
			}
		}
	}
}
