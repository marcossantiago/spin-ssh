package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// ErrCancelled is returned when the user presses esc or ctrl+c.
var ErrCancelled = errors.New("cancelled")

// Run shows a spinner with title while fn runs. ctrl+c cancels fn's context.
// Without a terminal (scripts, CI) it prints the title and runs fn plainly.
func Run(title string, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprintln(os.Stderr, title)
		return fn(ctx)
	}
	m := &task{
		title:  title,
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(currentTheme().fg(currentTheme().accent))),
		run:    func() tea.Msg { return taskDone{fn(ctx)} },
		cancel: cancel,
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	if m.cancelled {
		return ErrCancelled
	}
	return m.err
}

type taskDone struct{ err error }

type task struct {
	title     string
	spin      spinner.Model
	run       tea.Cmd
	cancel    context.CancelFunc
	err       error
	done      bool
	cancelled bool
}

func (m *task) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.run) }

func (m *task) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case taskDone:
		m.err, m.done = msg.err, true
		return m, tea.Quit
	case tea.KeyPressMsg:
		if s := msg.String(); s == "ctrl+c" || s == "esc" {
			m.cancel()
			m.cancelled, m.done = true, true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *task) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	return tea.NewView(m.spin.View() + " " + currentTheme().fg(currentTheme().dim).Render(m.title) + versionTag())
}

// Ask prompts for one line of text. It returns ErrCancelled on esc or ctrl+c.
func Ask(question, placeholder string) (string, error) {
	return ask1(question, placeholder, false)
}

// AskSecret is Ask with the typed text masked, for tokens.
func AskSecret(question string) (string, error) {
	return ask1(question, "", true)
}

func ask1(question, placeholder string, secret bool) (string, error) {
	t := currentTheme()
	in := textinput.New()
	in.Prompt = "❯ "
	in.Placeholder = placeholder
	styles := textinput.DefaultStyles(true)
	styles.Focused.Prompt = t.fg(t.accent).Bold(true)
	styles.Focused.Text = t.fg(t.text)
	styles.Focused.Placeholder = t.fg(t.faint)
	styles.Cursor.Color = t.accent
	in.SetStyles(styles)
	if secret {
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
	}
	m := &ask{question: question, input: in}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return "", err
	}
	if m.cancelled {
		return "", ErrCancelled
	}
	return m.input.Value(), nil
}

type ask struct {
	question  string
	input     textinput.Model
	done      bool
	cancelled bool
}

func (m *ask) Init() tea.Cmd { return m.input.Focus() }

func (m *ask) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			m.done = true
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.done, m.cancelled = true, true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *ask) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	return tea.NewView(currentTheme().fg(currentTheme().text).Bold(true).Render(m.question) + versionTag() + "\n" + m.input.View())
}

// Confirm asks a yes/no question; enter takes the default. It returns ErrCancelled
// on esc or ctrl+c.
func Confirm(question string, def bool) (bool, error) {
	m := &confirm{question: question, answer: def}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return false, err
	}
	if m.cancelled {
		return false, ErrCancelled
	}
	return m.answer, nil
}

type confirm struct {
	question  string
	answer    bool
	done      bool
	cancelled bool
}

func (m *confirm) Init() tea.Cmd { return nil }

func (m *confirm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch strings.ToLower(key.String()) {
	case "y":
		m.answer, m.done = true, true
	case "n":
		m.answer, m.done = false, true
	case "enter":
		m.done = true
	case "ctrl+c", "esc":
		m.done, m.cancelled = true, true
	default:
		return m, nil
	}
	return m, tea.Quit
}

func (m *confirm) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	t := currentTheme()
	hint := "y/N"
	if m.answer {
		hint = "Y/n"
	}
	return tea.NewView(t.fg(t.text).Bold(true).Render(m.question) + " " + t.fg(t.dim).Render(hint) + versionTag())
}

// versionTag is the version, faint, for the end of a one-line screen.
func versionTag() string {
	if Version == "" {
		return ""
	}
	return "  " + currentTheme().fg(currentTheme().faint).Render("spin-ssh "+Version)
}

// Key is one choice for Keys: the key to press and what it does.
type Key struct {
	Key  string // as tea.KeyPressMsg.String() names it: "enter", "c", "esc"
	Does string
}

// Keys waits for one of keys and returns its Key. ctrl+c returns ErrCancelled.
func Keys(keys ...Key) (string, error) {
	m := &keysModel{keys: keys}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return "", err
	}
	if m.pressed == "" {
		return "", ErrCancelled
	}
	return m.pressed, nil
}

type keysModel struct {
	keys    []Key
	pressed string
	done    bool
}

func (m *keysModel) Init() tea.Cmd { return nil }

func (m *keysModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+c" {
		m.done = true
		return m, tea.Quit
	}
	for _, k := range m.keys {
		if strings.EqualFold(key.String(), k.Key) {
			m.pressed, m.done = k.Key, true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *keysModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	t := currentTheme()
	parts := make([]string, len(m.keys))
	for i, k := range m.keys {
		parts[i] = t.fg(t.text).Bold(true).Render(k.Key) + " " + t.fg(t.dim).Render(k.Does)
	}
	return tea.NewView("   " + strings.Join(parts, t.fg(t.faint).Render("  ·  ")) + versionTag())
}
