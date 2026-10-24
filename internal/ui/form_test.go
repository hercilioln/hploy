package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func newForm(fields ...Field) formModel {
	in := textinput.New()
	in.Prompt = ""
	return formModel{
		title:  "env demo",
		fields: fields,
		input:  in,
		width:  80,
		height: 24,
		newField: func(name string) Field {
			return Field{Name: name, Note: "added"}
		},
		generators: []Generator{
			{Label: "url-safe bytes", New: func() string { return "url-safe-value" }},
			{
				Label: "base64: + bytes",
				New:   func() string { return "base64:value" },
				Match: func(v string) bool { return strings.HasPrefix(v, "base64:") },
			},
		},
	}
}

func str(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func form(m tea.Model) formModel { return m.(formModel) }

// The whole point of the panel: a value already in the file is on screen and
// stays there untouched.
func TestFormKeepsValuesNobodyEdited(t *testing.T) {
	m := form(send(newForm(
		Field{Name: "APP_ENV", Value: "local"},
		Field{Name: "APP_DEBUG", Value: "true"},
	), key(tea.KeyCtrlS)))

	if !m.saved {
		t.Fatal("ctrl+s should save")
	}
	if m.fields[0].Value != "local" || m.fields[1].Value != "true" {
		t.Errorf("fields = %v", m.fields)
	}
}

func TestFormEditsTheFocusedField(t *testing.T) {
	m := form(send(newForm(
		Field{Name: "APP_ENV", Value: "local"},
		Field{Name: "APP_URL", Value: ""},
	),
		key(tea.KeyDown),
		key(tea.KeyEnter),
		str("https://x.dev"),
		key(tea.KeyEnter),
		key(tea.KeyCtrlS),
	))

	if m.fields[0].Value != "local" {
		t.Errorf("APP_ENV = %q, want it untouched", m.fields[0].Value)
	}
	if m.fields[1].Value != "https://x.dev" {
		t.Errorf("APP_URL = %q", m.fields[1].Value)
	}
}

// Editing starts from the current value, so correcting a typo does not mean
// retyping the whole thing.
func TestFormEditingStartsFromTheCurrentValue(t *testing.T) {
	m := form(send(newForm(Field{Name: "APP_NAME", Value: "demo"}),
		key(tea.KeyEnter),
		str("-two"),
		key(tea.KeyEnter),
		key(tea.KeyCtrlS),
	))

	if m.fields[0].Value != "demo-two" {
		t.Errorf("APP_NAME = %q", m.fields[0].Value)
	}
}

func TestFormEscCancelsOneEditOnly(t *testing.T) {
	m := form(send(newForm(Field{Name: "APP_NAME", Value: "demo"}),
		key(tea.KeyEnter),
		str("junk"),
		key(tea.KeyEsc),
	))

	if m.editing {
		t.Error("esc should leave edit mode")
	}
	if m.fields[0].Value != "demo" {
		t.Errorf("APP_NAME = %q, want the value from before editing", m.fields[0].Value)
	}
	if m.dirty {
		t.Error("a cancelled edit is not a change")
	}
}

// Enter walks down, so filling a long file is type-enter-type-enter.
func TestFormEnterAdvances(t *testing.T) {
	m := form(send(newForm(
		Field{Name: "A"}, Field{Name: "B"}, Field{Name: "C"},
	),
		key(tea.KeyEnter), str("1"), key(tea.KeyEnter),
		key(tea.KeyEnter), str("2"), key(tea.KeyEnter),
		key(tea.KeyCtrlS),
	))

	if m.fields[0].Value != "1" || m.fields[1].Value != "2" || m.fields[2].Value != "" {
		t.Errorf("fields = %v", m.fields)
	}
}

// Which shape is a question, not a guess: the same 32 bytes in the wrong
// shape is an application that will not boot.
func TestFormAsksWhichShapeToGenerate(t *testing.T) {
	open := form(send(newForm(
		Field{Name: "APP_KEY", Secret: true, Required: true},
		Field{Name: "APP_ENV", Value: "local"},
	), key(tea.KeyCtrlG)))

	if !open.picking {
		t.Fatal("ctrl+g should offer the shapes")
	}
	if open.fields[0].Value != "" {
		t.Error("nothing is generated until the shape is picked")
	}
	if view := open.View(); !strings.Contains(view, "generate APP_KEY as") ||
		!strings.Contains(view, "base64: + bytes") {
		t.Errorf("the shapes have to be on screen:\n%s", view)
	}

	m := form(send(open, key(tea.KeyDown), key(tea.KeyEnter), key(tea.KeyCtrlS)))
	if m.fields[0].Value != "base64:value" {
		t.Errorf("APP_KEY = %q, want the shape that was picked", m.fields[0].Value)
	}
	// Only the focused one: a value nobody asked for is a value nobody knows.
	if m.fields[1].Value != "local" {
		t.Errorf("APP_ENV = %q", m.fields[1].Value)
	}
}

func TestFormCancelsTheShapePicker(t *testing.T) {
	m := form(send(newForm(Field{Name: "APP_KEY"}),
		key(tea.KeyCtrlG), key(tea.KeyEsc)))

	if m.picking {
		t.Error("esc should close the picker")
	}
	if m.fields[0].Value != "" || m.dirty {
		t.Errorf("value = %q, dirty = %v — a cancelled pick changes nothing",
			m.fields[0].Value, m.dirty)
	}
}

// Regenerating an existing value starts on the shape it already has, so the
// shape an application accepted once is not silently swapped.
func TestFormPickerStartsOnTheCurrentShape(t *testing.T) {
	m := form(send(newForm(Field{Name: "APP_KEY", Value: "base64:oldkey", Secret: true}),
		key(tea.KeyCtrlG)))

	if m.pick != 1 {
		t.Errorf("pick = %d, want the base64 shape", m.pick)
	}

	done := form(send(m, key(tea.KeyEnter)))
	if done.fields[0].Value != "base64:value" {
		t.Errorf("APP_KEY = %q", done.fields[0].Value)
	}
}

func TestFormPickerFitsTheWindow(t *testing.T) {
	fields := make([]Field, 40)
	for i := range fields {
		fields[i] = Field{Name: "K"}
	}
	m := newForm(fields...)
	m.height = 20

	open := form(send(m, key(tea.KeyCtrlG)))
	if lines := strings.Count(open.View(), "\n"); lines > open.height {
		t.Errorf("the picker pushed the view to %d lines in a %d-line terminal",
			lines, open.height)
	}
}

// Esc with unsaved work asks first, because the panel is the only copy of
// what was just typed.
func TestFormConfirmsBeforeDiscarding(t *testing.T) {
	m := form(send(newForm(Field{Name: "A"}),
		key(tea.KeyEnter), str("x"), key(tea.KeyEnter),
		key(tea.KeyEsc),
	))

	if !m.confirm || m.saved {
		t.Fatalf("confirm = %v, saved = %v", m.confirm, m.saved)
	}
	if m.message == "" {
		t.Error("the confirmation has to be visible")
	}

	// Saying so twice goes through.
	m = form(send(m, key(tea.KeyEsc)))
	if m.saved {
		t.Error("a discarded panel must not report itself saved")
	}
}

func TestFormEscLeavesImmediatelyWhenNothingChanged(t *testing.T) {
	m := form(send(newForm(Field{Name: "A", Value: "1"}), key(tea.KeyEsc)))
	if m.confirm {
		t.Error("nothing to confirm when nothing was edited")
	}
	if m.saved {
		t.Error("esc is not a save")
	}
}

func TestFormMasksSecretsUntilRevealed(t *testing.T) {
	m := newForm(Field{Name: "DB_PASSWORD", Value: "s3cret", Secret: true})

	if view := m.View(); strings.Contains(view, "s3cret") {
		t.Error("a secret must not be on screen by default")
	}
	if view := form(send(m, tea.KeyMsg{Type: tea.KeyCtrlR})).View(); !strings.Contains(view, "s3cret") {
		t.Error("ctrl+r should reveal it")
	}
}

func TestFormReportsEmptyRequiredFields(t *testing.T) {
	m := newForm(
		Field{Name: "APP_KEY", Required: true},
		Field{Name: "APP_ENV", Value: "local", Required: true},
	)
	if !strings.Contains(m.View(), "1 required still empty") {
		t.Errorf("view does not count the gap:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "required") {
		t.Error("the empty field needs its own marker")
	}
}

// A long list scrolls instead of printing a thousand rows.
func TestFormScrollsAWindow(t *testing.T) {
	fields := make([]Field, 60)
	for i := range fields {
		fields[i] = Field{Name: string(rune('A' + i%26))}
	}
	m := newForm(fields...)
	m.height = 20

	rows := m.rows()
	if rows >= len(fields) {
		t.Fatalf("rows = %d, the window has to be smaller than the list", rows)
	}
	if lines := strings.Count(m.View(), "\n"); lines > m.height {
		t.Errorf("the view is %d lines in a %d-line terminal", lines, m.height)
	}

	m = form(send(m, key(tea.KeyEnd)))
	if m.cursor != len(fields)-1 {
		t.Errorf("cursor = %d", m.cursor)
	}
	if m.offset == 0 {
		t.Error("the window did not follow the cursor")
	}

	// Clamped, not wrapped: holding the arrow down stops at the end.
	m = form(send(m, key(tea.KeyDown), key(tea.KeyDown)))
	if m.cursor != len(fields)-1 {
		t.Errorf("cursor = %d after running past the end", m.cursor)
	}
}

// An env file is not closed: a deployment regularly needs a key no example
// mentions.
func TestFormAddsAVariable(t *testing.T) {
	m := form(send(newForm(Field{Name: "APP_ENV", Value: "local"}),
		tea.KeyMsg{Type: tea.KeyCtrlN},
		str("SENTRY_DSN"),
		key(tea.KeyEnter),
		str("https://x@sentry.io/1"),
		key(tea.KeyEnter),
		key(tea.KeyCtrlS),
	))

	if len(m.fields) != 2 {
		t.Fatalf("fields = %v", m.fields)
	}
	if m.fields[1].Name != "SENTRY_DSN" || m.fields[1].Value != "https://x@sentry.io/1" {
		t.Errorf("added %q = %q", m.fields[1].Name, m.fields[1].Value)
	}
	// Naming a variable is never the point, so it lands in the value.
	if m.fields[0].Value != "local" {
		t.Errorf("APP_ENV = %q", m.fields[0].Value)
	}
}

func TestFormRefusesABadVariableName(t *testing.T) {
	m := form(send(newForm(Field{Name: "TAKEN", Value: "x"}),
		tea.KeyMsg{Type: tea.KeyCtrlN}, str("2COOL"), key(tea.KeyEnter)))
	if len(m.fields) != 1 || !m.naming {
		t.Errorf("a name starting with a digit was accepted: %v", m.fields)
	}
	if m.message == "" {
		t.Error("the reason has to be on screen")
	}

	dup := form(send(newForm(Field{Name: "TAKEN", Value: "x"}),
		tea.KeyMsg{Type: tea.KeyCtrlN}, str("TAKEN"), key(tea.KeyEnter)))
	if len(dup.fields) != 1 {
		t.Errorf("a duplicate was added: %v", dup.fields)
	}
	if !strings.Contains(dup.message, "already on the list") {
		t.Errorf("message = %q", dup.message)
	}
}

func TestFormCancelsAddingAVariable(t *testing.T) {
	m := form(send(newForm(Field{Name: "APP_ENV", Value: "local"}),
		tea.KeyMsg{Type: tea.KeyCtrlN}, str("NEW"), key(tea.KeyEsc)))

	if m.naming || len(m.fields) != 1 || m.dirty {
		t.Errorf("naming = %v, fields = %v, dirty = %v", m.naming, m.fields, m.dirty)
	}
}

func TestFormHidesAddWhenTheCallerHasNoFieldFactory(t *testing.T) {
	m := newForm(Field{Name: "A"})
	m.newField = nil

	if strings.Contains(m.View(), "ctrl+n") {
		t.Error("a binding that does nothing must not be advertised")
	}
	after := form(send(m, tea.KeyMsg{Type: tea.KeyCtrlN}))
	if after.naming {
		t.Error("ctrl+n should do nothing without a field factory")
	}
}
