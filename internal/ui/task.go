package ui

import (
	"context"
	"errors"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// ErrCancelled is returned when the user presses esc or ctrl+c.
var ErrCancelled = errors.New("cancelled")

// Run shows a spinner with title while fn runs. ctrl+c cancels fn's context.
func Run(title string, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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

// versionTag is the version, faint, for the end of a one-line screen.
func versionTag() string {
	if Version == "" {
		return ""
	}
	return "  " + currentTheme().fg(currentTheme().faint).Render("spin-ssh "+Version)
}
