package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// A Bubble Tea model is a pure function of its messages, so the keystrokes
// can be sent directly and no terminal is involved.
func send(m tea.Model, keys ...tea.KeyMsg) tea.Model {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

func typing(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func newInput(def string, secret bool, validate func(string) error) inputModel {
	ti := textinput.New()
	ti.Placeholder = def
	ti.Prompt = ""
	ti.Focus()
	if secret {
		ti.EchoMode = textinput.EchoPassword
	}
	return inputModel{label: "name", input: ti, validate: validate}
}

func TestInputReturnsWhatWasTyped(t *testing.T) {
	m := send(newInput("", false, nil), typing("nagarrafa"), key(tea.KeyEnter)).(inputModel)
	if !m.done || m.answer != "nagarrafa" {
		t.Errorf("answer = %q, done = %v", m.answer, m.done)
	}
}

// An empty answer takes the default, which is what makes a wizard a row of
// enters for the common case.
func TestEmptyInputTakesTheDefault(t *testing.T) {
	m := send(newInput("512m", false, nil), key(tea.KeyEnter)).(inputModel)
	if m.answer != "512m" {
		t.Errorf("answer = %q, want the default", m.answer)
	}
}

func TestValidationKeepsTheQuestionOpen(t *testing.T) {
	onlyDigits := func(s string) error {
		if strings.Trim(s, "0123456789") != "" {
			return fmt.Errorf("digits only")
		}
		return nil
	}

	m := send(newInput("", false, onlyDigits), typing("abc"), key(tea.KeyEnter)).(inputModel)
	if m.done {
		t.Fatal("an invalid answer must not finish the question")
	}
	if !strings.Contains(m.View(), "digits only") {
		t.Errorf("the reason should be on screen:\n%s", m.View())
	}

	// Correcting it moves on, without having restarted anything.
	m2 := send(m, key(tea.KeyBackspace), key(tea.KeyBackspace), key(tea.KeyBackspace),
		typing("8080"), key(tea.KeyEnter)).(inputModel)
	if !m2.done || m2.answer != "8080" {
		t.Errorf("answer = %q, done = %v", m2.answer, m2.done)
	}
}

func TestCtrlCAborts(t *testing.T) {
	m := send(newInput("", false, nil), typing("half"), key(tea.KeyCtrlC)).(inputModel)
	if !m.aborted || m.done {
		t.Error("ctrl+c should abort, not answer")
	}
}

func TestSecretIsNeverShown(t *testing.T) {
	m := send(newInput("", true, nil), typing("hunter2")).(inputModel)
	if strings.Contains(m.View(), "hunter2") {
		t.Error("the typed secret must not appear on screen")
	}

	done := send(m, key(tea.KeyEnter)).(inputModel)
	if done.answer != "hunter2" {
		t.Errorf("the value should still be captured, got %q", done.answer)
	}
	if strings.Contains(done.View(), "hunter2") {
		t.Error("the finished line must not echo the secret either")
	}
}

func TestSelectMovesWithArrows(t *testing.T) {
	options := []string{"PostgreSQL", "MySQL", "Redis"}
	m := selectModel{label: "which", options: options}

	got := send(m, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyEnter)).(selectModel)
	if !got.done || got.chosen != 2 {
		t.Errorf("chosen = %d, want 2", got.chosen)
	}

	// The cursor stops at the ends rather than wrapping or going out of range.
	top := send(m, key(tea.KeyUp), key(tea.KeyUp)).(selectModel)
	if top.cursor != 0 {
		t.Errorf("cursor = %d, want 0", top.cursor)
	}
	bottom := send(m, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown)).(selectModel)
	if bottom.cursor != len(options)-1 {
		t.Errorf("cursor = %d, want %d", bottom.cursor, len(options)-1)
	}
}

func TestSelectStartsOnTheDefault(t *testing.T) {
	m := selectModel{label: "add a service?", options: []string{"yes", "no"}, cursor: 1}
	got := send(m, key(tea.KeyEnter)).(selectModel)
	if got.chosen != 1 {
		t.Errorf("chosen = %d, want the default 1 (no)", got.chosen)
	}
}

func TestFinishedQuestionLeavesOneLine(t *testing.T) {
	m := send(newInput("", false, nil), typing("demo.localhost"), key(tea.KeyEnter)).(inputModel)
	view := m.View()
	if strings.Count(view, "\n") != 1 {
		t.Errorf("an answered question should leave exactly one line:\n%q", view)
	}
	if !strings.Contains(view, "demo.localhost") {
		t.Errorf("the answer should be visible:\n%s", view)
	}
}

// esc is a direction, not a failure: the wizard above decides what going back
// means, so the question only has to say which way it went.
func TestEscGoesBackWhenOffered(t *testing.T) {
	m := send(backInput(), typing("half"), key(tea.KeyEsc)).(inputModel)
	if !m.back || m.aborted || m.done {
		t.Errorf("back = %v, aborted = %v, done = %v", m.back, m.aborted, m.done)
	}
}

func TestEscCancelsWhenThereIsNoBack(t *testing.T) {
	m := send(newInput("", false, nil), key(tea.KeyEsc)).(inputModel)
	if !m.aborted || m.back {
		t.Errorf("aborted = %v, back = %v", m.aborted, m.back)
	}
}

// An abandoned question has to leave the screen as it found it, or the line
// the wizard goes back to would not be the last one any more.
func TestAbandonedQuestionLeavesNothing(t *testing.T) {
	back := send(backInput(), key(tea.KeyEsc)).(inputModel)
	if view := back.View(); view != "" {
		t.Errorf("a step back rendered %q", view)
	}

	cancelled := send(newInput("", false, nil), key(tea.KeyCtrlC)).(inputModel)
	if view := cancelled.View(); view != "" {
		t.Errorf("a cancelled question rendered %q", view)
	}

	sel := send(selectModel{label: "which", options: []string{"a", "b"}, allowsBack: true},
		key(tea.KeyEsc)).(selectModel)
	if !sel.back {
		t.Fatal("esc should step back out of a select too")
	}
	if view := sel.View(); view != "" {
		t.Errorf("a step back rendered %q", view)
	}
}

func TestSelectSaysWhenBackIsAvailable(t *testing.T) {
	with := selectModel{label: "which", options: []string{"a"}, allowsBack: true}
	if !strings.Contains(with.View(), "esc back") {
		t.Errorf("the binding has to be on screen:\n%s", with.View())
	}
	without := selectModel{label: "which", options: []string{"a"}}
	if strings.Contains(without.View(), "esc back") {
		t.Error("a question with nowhere to go back to must not offer it")
	}
}

func backInput() inputModel {
	m := newInput("", false, nil)
	m.allowsBack = true
	return m
}
