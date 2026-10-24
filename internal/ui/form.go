package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Field is one named value in a Form.
type Field struct {
	Name     string
	Value    string
	Note     string // a short annotation shown on the right
	Secret   bool   // masked in the list
	Required bool   // empty is reported, never refused
}

// FormOptions configures the panel around the fields.
type FormOptions struct {
	Title string
	Note  string // a line under the title, usually the file being edited

	// Generators are the shapes ctrl+g can produce. An empty list hides the
	// binding; entropy is the caller's business, not the view's.
	Generators []Generator

	// NewField builds the row for a name added with ctrl+n. Nil hides the
	// binding. The caller decides what a name means -- whether it looks like
	// a credential, for one -- since the panel only knows how to draw it.
	NewField func(name string) Field
}

// Generator is one shape a generated value can take.
//
// hploy offers the shapes instead of picking one, because a value that looks
// random is not interchangeable with another that looks random: an
// application decides what it will accept, and the panel cannot know.
type Generator struct {
	Label string
	New   func() string

	// Match recognises a value this generator produced, so regenerating a
	// field starts on the shape it already has.
	Match func(string) bool
}

// Form edits a list of named values in one screen.
//
// A long env file is a list, not an interview: asking fifty questions in a
// row forces an answer to each one, while a list shows what is already there
// and lets someone touch only what is wrong.
func Form(fields []Field, opt FormOptions) ([]Field, error) {
	if len(fields) == 0 {
		return nil, nil
	}

	in := textinput.New()
	in.Prompt = ""

	m := formModel{
		title:      opt.Title,
		note:       opt.Note,
		generators: opt.Generators,
		newField:   opt.NewField,
		// Copied: an abort must leave the caller's slice untouched.
		fields: append([]Field(nil), fields...),
		input:  in,
		width:  80,
		height: 24,
	}

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	out := final.(formModel)
	if !out.saved {
		return nil, ErrCancelled
	}
	return out.fields, nil
}

type formModel struct {
	title      string
	note       string
	generators []Generator
	newField   func(string) Field

	fields []Field
	cursor int
	offset int // first visible row

	width  int
	height int

	editing bool
	input   textinput.Model
	picking bool // the generator list is open over the footer
	pick    int
	naming  bool // ctrl+n is asking for a name
	reveal  bool
	dirty   bool
	message string
	confirm bool // esc was pressed with unsaved changes

	saved bool
}

func (m formModel) Init() tea.Cmd { return textinput.Blink }

func (m formModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch {
		case m.naming:
			return m.nameKey(msg)
		case m.editing:
			return m.editKey(msg)
		case m.picking:
			return m.pickKey(msg)
		}
		return m.navKey(msg)
	}

	if m.editing {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m formModel) navKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any keystroke answers the pending discard question.
	if m.confirm && key.String() != "esc" {
		m.confirm, m.message = false, ""
	}

	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		if m.dirty && !m.confirm {
			m.confirm = true
			m.message = "unsaved changes — esc again to discard, ctrl+s to save"
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+s":
		m.saved = true
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.rows())
	case "pgdown":
		m.move(m.rows())
	case "home", "g":
		m.move(-len(m.fields))
	case "end", "G":
		m.move(len(m.fields))
	case "ctrl+r":
		m.reveal = !m.reveal
	case "ctrl+g":
		switch len(m.generators) {
		case 0:
		case 1:
			m = m.generate(0)
		default:
			// Which shape is a question, so it gets asked.
			m.picking = true
			m.pick = m.shapeOf(m.fields[m.cursor].Value)
			m.message = ""
			m.move(0) // the list is shorter now; keep the cursor in it
		}
	case "ctrl+n":
		if m.newField != nil {
			m.naming = true
			m.input.SetValue("")
			m.input.Placeholder = "NAME"
			m.input.Focus()
			m.message = ""
			return m, textinput.Blink
		}
	case "enter", " ":
		return m.startEdit(), textinput.Blink
	}
	return m, nil
}

// nameKey adds a variable the project never declared. An env file is not
// closed: a deployment regularly needs a key no example mentions.
func (m formModel) nameKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.naming, m.message = false, ""
		return m, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(m.input.Value())
		if err := m.checkName(name); err != nil {
			m.message = err.Error()
			return m, nil
		}
		m.fields = append(m.fields, m.newField(name))
		m.naming, m.message = false, ""
		m.dirty = true
		m.cursor = len(m.fields) - 1
		m.move(0)
		// Straight into the value: naming a variable is never the point.
		return m.startEdit(), textinput.Blink
	}

	m.message = ""
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

var reEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (m formModel) checkName(name string) error {
	if name == "" {
		return fmt.Errorf("a name is required")
	}
	if !reEnvName.MatchString(name) {
		return fmt.Errorf("%q is not a variable name: letters, digits and _, not starting with a digit", name)
	}
	for _, f := range m.fields {
		if f.Name == name {
			return fmt.Errorf("%s is already on the list", name)
		}
	}
	return nil
}

func (m formModel) editKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		// The value on screen before editing started is kept.
		m.editing = false
		return m, nil
	case tea.KeyEnter, tea.KeyTab:
		m = m.commit()
		// Enter walks down the list, so filling many in a row needs no
		// second keystroke between them.
		m.move(1)
		if key.Type == tea.KeyTab {
			return m.startEdit(), textinput.Blink
		}
		return m, nil
	case tea.KeyShiftTab:
		m = m.commit()
		m.move(-1)
		return m.startEdit(), textinput.Blink
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m formModel) pickKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "ctrl+g":
		m.picking = false
	case "up", "k":
		if m.pick > 0 {
			m.pick--
		}
	case "down", "j":
		if m.pick < len(m.generators)-1 {
			m.pick++
		}
	case "enter", " ":
		m = m.generate(m.pick)
		m.picking = false
	}
	return m, nil
}

func (m formModel) generate(i int) formModel {
	m.fields[m.cursor].Value = m.generators[i].New()
	m.fields[m.cursor].Note = "generated"
	m.dirty = true
	return m
}

// shapeOf finds the generator a value already came from, so regenerating
// keeps the shape the application accepted last time.
func (m formModel) shapeOf(value string) int {
	if value == "" {
		return 0
	}
	for i, g := range m.generators {
		if g.Match != nil && g.Match(value) {
			return i
		}
	}
	return 0
}

func (m formModel) startEdit() formModel {
	m.input.SetValue(m.fields[m.cursor].Value)
	m.input.Placeholder = ""
	m.input.Width = m.valueWidth(m.nameWidth())
	// Masked in the list and masked while being typed. Editing is not a
	// reason to put a password on a shared screen or into an SSH recording;
	// ctrl+r is how someone says they want to see it.
	if m.fields[m.cursor].Secret && !m.reveal {
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
	} else {
		m.input.EchoMode = textinput.EchoNormal
	}
	m.input.CursorEnd()
	m.input.Focus()
	m.editing = true
	m.message = ""
	return m
}

func (m formModel) commit() formModel {
	value := strings.TrimSpace(m.input.Value())
	if value != m.fields[m.cursor].Value {
		m.fields[m.cursor].Value = value
		m.fields[m.cursor].Note = "edited"
		m.dirty = true
	}
	m.editing = false
	return m
}

// move clamps instead of wrapping: at the end of a long list, holding the
// arrow down should stop, not jump back to the top.
func (m *formModel) move(by int) {
	m.cursor += by
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.fields) {
		m.cursor = len(m.fields) - 1
	}

	rows := m.rows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// rows is how many fields fit between the header and the footer.
func (m formModel) rows() int {
	n := m.height - 7
	switch {
	case m.picking:
		// The generator list sits where the footer's status line was.
		n -= len(m.generators)
	case m.naming:
		n -= 2
	}
	if n < 3 {
		return 3
	}
	return n
}

func (m formModel) View() string {
	var b strings.Builder

	fmt.Fprintf(&b, "  %s %s\n", Title.Render(m.title), Dim.Render(m.counts()))
	if m.note != "" {
		fmt.Fprintf(&b, "  %s\n", Dim.Render(elide(m.note, m.width-4)))
	}
	b.WriteString("\n")

	nameWidth := m.nameWidth()
	rows := m.rows()
	for i := m.offset; i < len(m.fields) && i < m.offset+rows; i++ {
		b.WriteString(m.row(i, nameWidth))
	}

	// A fixed-height list keeps the footer from jumping around as the window
	// scrolls through a short tail.
	for i := len(m.fields) - m.offset; i < rows; i++ {
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.naming {
		fmt.Fprintf(&b, "  %s %s %s\n", Title.Render("?"), "new variable",
			Dim.Render("enter to add · esc cancel"))
		fmt.Fprintf(&b, "  %s %s\n", Dim.Render("›"), m.input.View())
		if m.message != "" {
			fmt.Fprintf(&b, "    %s\n", Bad.Render(m.message))
		}
		return b.String()
	}
	if m.picking {
		fmt.Fprintf(&b, "  %s %s\n", Title.Render("?"),
			"generate "+m.fields[m.cursor].Name+" as")
		for i, g := range m.generators {
			if i == m.pick {
				fmt.Fprintf(&b, "  %s %s\n", Good.Render("›"), Ours.Render(g.Label))
				continue
			}
			fmt.Fprintf(&b, "    %s\n", Dim.Render(g.Label))
		}
	} else if m.message != "" {
		fmt.Fprintf(&b, "  %s\n", Warn.Render(m.message))
	} else {
		fmt.Fprintf(&b, "  %s\n", Dim.Render(m.scrollNote()))
	}
	fmt.Fprintf(&b, "  %s\n", Dim.Render(m.keys()))
	return b.String()
}

func (m formModel) row(i int, nameWidth int) string {
	f := m.fields[i]
	focused := i == m.cursor

	marker := "  "
	name := pad(f.Name, nameWidth)
	switch {
	case focused && m.editing:
		marker = Title.Render("›") + " "
		name = Title.Render(name)
	case focused:
		marker = Good.Render("›") + " "
		name = Ours.Render(name)
	default:
		name = Dim.Render(name)
	}

	if m.editing && focused {
		return fmt.Sprintf("  %s%s %s\n", marker, name, m.input.View())
	}

	row := fmt.Sprintf("  %s%s %s", marker, name, m.value(f, m.valueWidth(nameWidth)))
	if badge := m.badge(f); badge != "" {
		row += "  " + badge
	}
	return row + "\n"
}

// value is padded to the column width so the badges line up: a column that
// moves with the longest value on screen is a column nobody can read down.
func (m formModel) value(f Field, width int) string {
	switch {
	case f.Value == "":
		return Dim.Render(pad("—", width))
	case f.Secret && !m.reveal:
		return Dim.Render(pad(strings.Repeat("•", 8), width))
	default:
		return pad(truncate(f.Value, width), width)
	}
}

func (m formModel) badge(f Field) string {
	if f.Required && f.Value == "" {
		return Bad.Render("required")
	}
	if f.Note == "" {
		return ""
	}
	return Dim.Render(f.Note)
}

// valueWidth is the widest value there is, within what the window leaves
// after the name and the badge. Measuring the content rather than filling
// the terminal keeps the badge next to the value instead of across the
// screen from it.
func (m formModel) valueWidth(nameWidth int) int {
	longest := 0
	for _, f := range m.fields {
		n := lipgloss.Width(f.Value)
		if f.Secret && !m.reveal {
			n = 8
		}
		if n > longest {
			longest = n
		}
	}

	available := m.width - nameWidth - 18
	if available < 12 {
		available = 12
	}
	if longest > available {
		return available
	}
	if longest < 12 {
		return 12
	}
	return longest
}

func (m formModel) nameWidth() int {
	longest := 0
	for _, f := range m.fields {
		if n := lipgloss.Width(f.Name); n > longest {
			longest = n
		}
	}
	if longest > 32 {
		return 32
	}
	return longest
}

func (m formModel) counts() string {
	empty := 0
	for _, f := range m.fields {
		if f.Required && f.Value == "" {
			empty++
		}
	}
	out := fmt.Sprintf("%d variables", len(m.fields))
	if empty > 0 {
		out += fmt.Sprintf(" · %d required still empty", empty)
	}
	if m.dirty {
		out += " · unsaved"
	}
	return out
}

func (m formModel) scrollNote() string {
	rows := m.rows()
	if len(m.fields) <= rows {
		return ""
	}
	last := m.offset + rows
	if last > len(m.fields) {
		last = len(m.fields)
	}
	return fmt.Sprintf("%d–%d of %d", m.offset+1, last, len(m.fields))
}

func (m formModel) keys() string {
	if m.picking {
		return "↑↓ move · enter generate · esc cancel"
	}
	if m.editing {
		// Editing starts at the end of the current value, so a typo costs one
		// keystroke; replacing it outright needs the clear.
		return "enter next · ctrl+u clear · tab next field · esc cancel"
	}
	keys := []string{"↑↓ move", "enter edit"}
	if m.newField != nil {
		keys = append(keys, "ctrl+n add")
	}
	if len(m.generators) > 0 {
		keys = append(keys, "ctrl+g generate")
	}
	if m.anySecret() {
		keys = append(keys, "ctrl+r reveal")
	}
	return strings.Join(append(keys, "ctrl+s save", "esc quit"), " · ")
}

func (m formModel) anySecret() bool {
	for _, f := range m.fields {
		if f.Secret {
			return true
		}
	}
	return false
}

// elide shortens from the left, because a path's tail is the part that
// identifies it.
func elide(s string, max int) string {
	r := []rune(s)
	if len(r) <= max || max < 2 {
		return s
	}
	return "…" + string(r[len(r)-max+1:])
}

// pad widens plain text. Styling has to come after, or the escape codes would
// be counted as width and the column would come out short.
func pad(s string, width int) string {
	if n := lipgloss.Width(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
