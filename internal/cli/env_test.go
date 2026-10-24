package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hercilioln/hploy/internal/detect"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
)

func loadSpecYAML(t *testing.T, yaml string) *spec.Spec {
	t.Helper()
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func byName(fields []ui.Field, name string) (ui.Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return ui.Field{}, false
}

func names(fields []ui.Field) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.Name)
	}
	return out
}

const envSpec = `version: 1
app: demo
web:
  build: {context: .}
  port: 9000
  resources: {memory: 512m}
  env:
    from_file: true
    required: [APP_KEY%s]
services:
  db:
    image: postgres:17-alpine
    resources: {memory: 256m}
    env:
      vars:
        POSTGRES_PASSWORD: ${DB_PASSWORD}
`

// demoSpec is envSpec with extra required keys, so one test can ask for a
// host variable without a second copy of the YAML.
func demoSpec(t *testing.T, alsoRequired ...string) *spec.Spec {
	t.Helper()
	extra := ""
	for _, k := range alsoRequired {
		extra += ", " + k
	}
	return loadSpecYAML(t, fmt.Sprintf(envSpec, extra))
}

// The example's own values are the starting point. Losing them was what made
// the old one-question-per-variable wizard worse than useless: fifty enters
// turned a set of sensible defaults into fifty blanks.
func TestEnvFieldsPrefillFromTheExample(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(
		"APP_NAME=Demo\nAPP_ENV=local\nAPP_KEY=\nDB_PASSWORD=\n"), 0o600)

	project, err := detect.Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := envFields(demoSpec(t), project, filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	f, ok := byName(fields, "APP_ENV")
	if !ok || f.Value != "local" {
		t.Errorf("APP_ENV = %q, %v", f.Value, ok)
	}
	if f.Note != ".env.example" {
		t.Errorf("note = %q — where the value came from has to be visible", f.Note)
	}

	// A blank in the example stays blank: nothing is invented.
	if f, _ := byName(fields, "APP_KEY"); f.Value != "" {
		t.Errorf("APP_KEY = %q, want empty", f.Value)
	}
}

func TestEnvFieldsMarkWhatTheSpecRequires(t *testing.T) {
	dir := t.TempDir()
	project, err := detect.Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := envFields(demoSpec(t), project, filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"APP_KEY", "DB_PASSWORD"} {
		f, ok := byName(fields, want)
		if !ok {
			t.Fatalf("%s is missing from %v", want, names(fields))
		}
		if !f.Required {
			t.Errorf("%s should be marked required", want)
		}
	}

	// A ${NAME} reference counts as required even though the spec never
	// lists it: the container cannot start without it either.
	if f, _ := byName(fields, "DB_PASSWORD"); !f.Secret {
		t.Error("DB_PASSWORD should be masked")
	}
}

// A database service is reachable at its own name, so that is the answer.
func TestEnvFieldsSuggestTheServiceAlias(t *testing.T) {
	dir := t.TempDir()
	project, err := detect.Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := envFields(demoSpec(t, "DB_HOST"), project,
		filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	f, ok := byName(fields, "DB_HOST")
	if !ok || f.Value != "db" {
		t.Errorf("DB_HOST = %q, %v", f.Value, ok)
	}
}

// What is already set wins over the example, and keeps its place in the file.
func TestEnvFieldsPreferTheFileAndItsOrder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("APP_NAME=Demo\nAPP_ENV=local\n"), 0o600)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_ENV=production\n"), 0o600)

	project, err := detect.Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := envFields(demoSpec(t), project, filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	if fields[0].Name != "APP_ENV" {
		t.Errorf("first field = %q, want the file's own order first", fields[0].Name)
	}
	if fields[0].Value != "production" {
		t.Errorf("APP_ENV = %q, want the value in the file", fields[0].Value)
	}
	if fields[0].Note != "" {
		t.Errorf("note = %q, want none: it is not a suggestion", fields[0].Note)
	}
}

func TestEnvFieldsListEachKeyOnce(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("APP_KEY=\nDB_PASSWORD=\n"), 0o600)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=set\n"), 0o600)

	project, err := detect.Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := envFields(demoSpec(t), project, filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]int{}
	for _, f := range fields {
		seen[f.Name]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times", name, n)
		}
	}
}

// An empty value is not written as "KEY=", or plan would see the variable as
// set and stop reporting a missing APP_KEY.
func TestSavingSkipsEmptyKeysNotAlreadyInTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("WAS_THERE=\n"), 0o600)

	saved, err := saveFields(path, []ui.Field{
		{Name: "APP_KEY", Value: "k"},
		{Name: "NEVER_SET", Value: ""},
		{Name: "WAS_THERE", Value: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved != 2 {
		t.Errorf("saved %d, want 2", saved)
	}

	back, err := store.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := back["NEVER_SET"]; ok {
		t.Error("an empty new key must stay out of the file")
	}
	if _, ok := back["WAS_THERE"]; !ok {
		t.Error("a key already in the file is not removed by clearing it")
	}
	if back["APP_KEY"] != "k" {
		t.Errorf("APP_KEY = %q", back["APP_KEY"])
	}
}
