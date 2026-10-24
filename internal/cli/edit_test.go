package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodSpec = `version: 1
app: demo
web:
  image: nginx:1.29-alpine
  port: 80
  resources: {memory: 128m}
`

// An editor that rewrites the file, so the loop can be driven without a
// terminal.
func fakeEditor(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", path)
	t.Setenv("VISUAL", "")
}

func specFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hploy.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEditSavesAValidChange(t *testing.T) {
	fakeEditor(t, `sed -i 's/128m/512m/' "$1"`)
	path := specFile(t, goodSpec)

	if err := editSpec(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "512m") {
		t.Errorf("the edit was not kept:\n%s", after)
	}
}

func TestEditNoticesNothingChanged(t *testing.T) {
	fakeEditor(t, "true")
	path := specFile(t, goodSpec)

	if err := editSpec(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != goodSpec {
		t.Errorf("the file was rewritten:\n%s", after)
	}
}

// A spec that does not validate must not reach the store: it would fail the
// next apply instead of this edit.
func TestEditRefusesToSaveABrokenSpec(t *testing.T) {
	fakeEditor(t, `sed -i 's/128m/128/' "$1"`)
	path := specFile(t, goodSpec)

	// No terminal, so there is nobody to ask what to do with it.
	err := editSpec(path)
	if err == nil {
		t.Fatal("a spec that does not validate was accepted")
	}
	if !strings.Contains(err.Error(), "reverted") {
		t.Errorf("err = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != goodSpec {
		t.Errorf("a spec Docker would refuse was left in place:\n%s", after)
	}
}

func TestRunEditorRefusesWhenThereIsNone(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("PATH", t.TempDir()) // no nano, vim or vi to fall back to

	err := runEditor(specFile(t, goodSpec))
	if err == nil || !strings.Contains(err.Error(), "no editor found") {
		t.Errorf("err = %v", err)
	}
}

// EDITOR can carry flags, like "code -w".
func TestEditorCommandCanHaveArguments(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$1 $2\" > "+marker+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script+" --wait")

	path := specFile(t, goodSpec)
	if err := runEditor(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(marker)
	if strings.TrimSpace(string(got)) != "--wait "+path {
		t.Errorf("the editor got %q", got)
	}
}
