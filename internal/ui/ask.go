package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ErrCancelled is returned when someone presses ctrl+c, or esc on a question
// that has nowhere to step back to.
var ErrCancelled = fmt.Errorf("cancelled")

// ErrBack is returned when someone steps out of a question with esc and the
// caller offered to go back. It is a direction, not a failure.
var ErrBack = fmt.Errorf("back")

// ErrSkip is what a step returns when it has nothing to ask -- an answer the
// project already gave, or a question the previous answer made irrelevant.
// The wizard then carries on the way it was already going, so stepping back
// through it does not bounce forward again.
var ErrSkip = fmt.Errorf("skip")

// Each question is its own short-lived Bubble Tea program, with no alternate
// screen: answers stay on the terminal as a transcript of what was decided.
//
// An abandoned question renders nothing at all. That is what lets a wizard
// step backwards: the question leaves no line behind, so the one before it is
// the last thing on screen again.

// Ask is one question.
type Ask struct {
	Label    string
	Hint     string
	Default  string // taken when the answer is empty
	Secret   bool
	Back     bool // esc steps back instead of cancelling
	Validate func(string) error
}

// Choice is one question answered from a list.
type Choice struct {
	Label   string
	Options []string
	Default int
	Back    bool
}

type inputModel struct {
	label      string
	hint       string
	input      textinput.Model
	validate   func(string) error
	allowsBack bool
	problem    string
	answer     string
	done       bool
	aborted    bool
	back       bool
}

func (m inputModel) Init() tea.Cmd { return textinput.Blink }

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch key.Type {
	case tea.KeyCtrlC:
		m.aborted = true
		return m, tea.Quit
	case tea.KeyEsc:
		m.back, m.aborted = m.allowsBack, !m.allowsBack
		return m, tea.Quit
	case tea.KeyEnter:
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			value = m.input.Placeholder
		}
		if m.validate != nil {
			if err := m.validate(value); err != nil {
				m.problem = err.Error()
				return m, nil
			}
		}
		m.answer, m.done = value, true
		return m, tea.Quit
	}

	m.problem = ""
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	if m.aborted || m.back {
		return ""
	}
	if m.done {
		return answered(m.label, m.shownAnswer())
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s", Title.Render("?"), m.label)
	if hint := joinHints(m.hint, m.allowsBack); hint != "" {
		b.WriteString(" " + Dim.Render(hint))
	}
	b.WriteString("\n  " + Dim.Render("›") + " " + m.input.View() + "\n")
	if m.problem != "" {
		b.WriteString("    " + Bad.Render(m.problem) + "\n")
	}
	return b.String()
}

func (m inputModel) shownAnswer() string {
	if m.input.EchoMode == textinput.EchoPassword {
		return strings.Repeat("•", 8)
	}
	if m.answer == "" {
		return Dim.Render("(empty)")
	}
	return m.answer
}

// answered is the one line a finished question leaves behind.
func answered(label, value string) string {
	return fmt.Sprintf("  %s %s %s\n", Good.Render("✓"), Dim.Render(label), value)
}

func joinHints(hint string, back bool) string {
	parts := []string{}
	if hint != "" {
		parts = append(parts, hint)
	}
	if back {
		parts = append(parts, "esc back")
	}
	return strings.Join(parts, " · ")
}

// Input asks for a line of text. An empty answer takes the default.
func Input(a Ask) (string, error) {
	ti := textinput.New()
	ti.Placeholder = a.Default
	ti.Prompt = ""
	ti.Focus()
	if a.Secret {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}

	final, err := tea.NewProgram(inputModel{
		label: a.Label, hint: a.Hint, input: ti,
		validate: a.Validate, allowsBack: a.Back,
	}).Run()
	if err != nil {
		return "", err
	}
	m := final.(inputModel)
	switch {
	case m.back:
		return "", ErrBack
	case m.aborted:
		return "", ErrCancelled
	}
	return m.answer, nil
}

type selectModel struct {
	label      string
	options    []string
	cursor     int
	chosen     int
	allowsBack bool
	done       bool
	aborted    bool
	back       bool
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		m.aborted = true
		return m, tea.Quit
	case "esc":
		m.back, m.aborted = m.allowsBack, !m.allowsBack
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case "enter", " ":
		m.chosen, m.done = m.cursor, true
		return m, tea.Quit
	}
	return m, nil
}

func (m selectModel) View() string {
	if m.aborted || m.back {
		return ""
	}
	if m.done {
		return answered(m.label, m.options[m.chosen])
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s %s\n", Title.Render("?"), m.label,
		Dim.Render(joinHints("↑↓ to move, enter to pick", m.allowsBack)))
	for i, o := range m.options {
		if i == m.cursor {
			fmt.Fprintf(&b, "  %s %s\n", Good.Render("›"), Ours.Render(o))
			continue
		}
		fmt.Fprintf(&b, "    %s\n", Dim.Render(o))
	}
	return b.String()
}

// Select presents a list navigated with the arrow keys.
func Select(c Choice) (int, error) {
	if c.Default < 0 || c.Default >= len(c.Options) {
		c.Default = 0
	}
	final, err := tea.NewProgram(selectModel{
		label: c.Label, options: c.Options, cursor: c.Default, allowsBack: c.Back,
	}).Run()
	if err != nil {
		return 0, err
	}
	m := final.(selectModel)
	switch {
	case m.back:
		return 0, ErrBack
	case m.aborted:
		return 0, ErrCancelled
	}
	return m.chosen, nil
}
