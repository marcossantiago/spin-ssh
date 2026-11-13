package ui

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Tone colours a value: a status, a badge, a detail field.
type Tone int

const (
	ToneNormal Tone = iota
	ToneDim
	ToneGood
	ToneWarn
	ToneBad
)

type theme struct {
	accent, text, dim, faint, border, selBg, good, warn, bad color.Color
}

var (
	themeOnce sync.Once
	th        theme
)

// currentTheme picks colours for the terminal's background, asked once per run.
func currentTheme() theme {
	themeOnce.Do(func() {
		ld := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
		th = theme{
			accent: ld(lipgloss.Color("#5B3FD6"), lipgloss.Color("#A594FF")),
			text:   ld(lipgloss.Color("#1F1F2B"), lipgloss.Color("#E6E6F0")),
			dim:    ld(lipgloss.Color("#77778A"), lipgloss.Color("#8C8CA3")),
			faint:  ld(lipgloss.Color("#A8A8B8"), lipgloss.Color("#5C5C70")),
			border: ld(lipgloss.Color("#D0D0DC"), lipgloss.Color("#3D3D4E")),
			selBg:  ld(lipgloss.Color("#ECE8FF"), lipgloss.Color("#2B2645")),
			good:   ld(lipgloss.Color("#1E8A3E"), lipgloss.Color("#6BD68F")),
			warn:   ld(lipgloss.Color("#9A6200"), lipgloss.Color("#E8C170")),
			bad:    ld(lipgloss.Color("#C0362C"), lipgloss.Color("#F2786D")),
		}
	})
	return th
}

func (t theme) fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func (t theme) tone(tn Tone) lipgloss.Style {
	switch tn {
	case ToneDim:
		return t.fg(t.dim)
	case ToneGood:
		return t.fg(t.good)
	case ToneWarn:
		return t.fg(t.warn)
	case ToneBad:
		return t.fg(t.bad)
	default:
		return t.fg(t.text)
	}
}

// Note prints a status line to stderr, outside any Bubble Tea program.
func Note(format string, args ...any) {
	t := currentTheme()
	fmt.Fprintln(os.Stderr, t.fg(t.accent).Render("›")+" "+fmt.Sprintf(format, args...))
}

// Success prints a line with a green check to stderr.
func Success(format string, args ...any) {
	t := currentTheme()
	fmt.Fprintln(os.Stderr, t.fg(t.good).Render("✓")+" "+fmt.Sprintf(format, args...))
}

// box draws a rounded box of exactly w×h cells, with a title on the top-left and a
// note on the top-right of its border. Body lines are clipped and padded to fit.
func box(w, h int, title, right string, body []string, border lipgloss.Style) string {
	iw := max(0, w-2)
	tw, rw := lipgloss.Width(title), lipgloss.Width(right)
	// "╭─ title ─…─ right ─╮": corners, two dashes, and a space each side of the labels.
	fill := w - 4 - (tw + 2) - (rw + 2)
	if right == "" {
		fill += 2
	}
	if fill < 1 && right != "" {
		right, fill = "", fill+rw+2
	}
	if fill < 1 {
		title = ansi.Truncate(title, max(0, tw+fill-1), "…")
		fill = 1
	}
	top := border.Render("╭─") + " " + title + " " + border.Render(strings.Repeat("─", fill))
	if right != "" {
		top += " " + right + " "
	}
	top += border.Render("─╮")

	lines := make([]string, 0, h)
	lines = append(lines, top)
	side := border.Render("│")
	for i := range max(0, h-2) {
		line := ""
		if i < len(body) {
			line = ansi.Truncate(body[i], iw, "…")
		}
		lines = append(lines, side+pad(line, iw)+side)
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return strings.Join(lines, "\n")
}

func pad(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// truncate shortens plain text to w cells, ending in "…".
func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return strings.Repeat("…", max(0, w))
	}
	return ansi.Truncate(s, w-1, "") + "…"
}
