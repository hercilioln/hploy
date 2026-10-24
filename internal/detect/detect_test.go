package detect

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExposedPorts(t *testing.T) {
	dir := t.TempDir()
	// Every shape a real Dockerfile uses, including ones to ignore.
	write(t, dir, "Dockerfile", `
FROM node:22-alpine
EXPOSE 3000
EXPOSE 8080/tcp 9090/udp
expose 443
EXPOSE ${PORT}
EXPOSE 99999
# EXPOSE 1234
EXPOSE 3000
`)
	got := exposedPorts(filepath.Join(dir, "Dockerfile"))
	want := []int{443, 3000, 8080, 9090}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestEnvDeclKeepsFileOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env.example", `
# a comment
APP_NAME=demo

export DB_HOST=localhost
DB_PASSWORD=
DB_PASSWORD=duplicate
not a pair
`)
	got, values := envDecl(filepath.Join(dir, ".env.example"))
	want := []string{"APP_NAME", "DB_HOST", "DB_PASSWORD"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	// The suggested defaults are the point: they become the starting values
	// in the editor instead of being retyped.
	if values["APP_NAME"] != "demo" || values["DB_HOST"] != "localhost" {
		t.Errorf("values = %v", values)
	}
	// A blank in the example is a blank to fill in, not a missing key.
	if v, ok := values["DB_PASSWORD"]; !ok || v != "" {
		t.Errorf("DB_PASSWORD = %q, %v", v, ok)
	}
}

func TestEnvDeclUnquotes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env.example", "A=\"two words\"\nB='single'\nC=pa$$word\n")

	_, values := envDecl(filepath.Join(dir, ".env.example"))
	if values["A"] != "two words" || values["B"] != "single" {
		t.Errorf("values = %v", values)
	}
	// Nothing but the outer quotes is interpreted, so a dollar survives.
	if values["C"] != "pa$$word" {
		t.Errorf("C = %q", values["C"])
	}
}

func TestInspectFindsTheConventions(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Dockerfile", "FROM python:3.13-alpine\nEXPOSE 8000\n")
	write(t, dir, "docker-compose.yml", "services: {}\n")
	write(t, dir, ".env.sample", "SECRET_KEY=\n")

	p, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Dockerfile != "Dockerfile" {
		t.Errorf("Dockerfile = %q", p.Dockerfile)
	}
	if len(p.Ports) != 1 || p.Ports[0] != 8000 {
		t.Errorf("Ports = %v, want [8000]", p.Ports)
	}
	if !p.HasCompose {
		t.Error("the compose file should be noticed")
	}
	if p.HasSpec {
		t.Error("there is no hploy.yaml here")
	}
	if p.EnvExample != ".env.sample" {
		t.Errorf("EnvExample = %q", p.EnvExample)
	}
	if len(p.EnvKeys) != 1 || p.EnvKeys[0] != "SECRET_KEY" {
		t.Errorf("EnvKeys = %v", p.EnvKeys)
	}
}

// An absent convention is an answer, not an error: plenty of projects have
// no Dockerfile at the root and no env example at all.
func TestInspectOnABareDirectory(t *testing.T) {
	p, err := Inspect(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Dockerfile != "" || p.EnvExample != "" || p.Repo != "" || p.HasSpec {
		t.Errorf("nothing should have been found: %+v", p)
	}
}

func TestGitDetection(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"remote", "add", "origin", "git@github.com:someone/thing.git"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(t, dir, ".gitignore", ".env\n")
	write(t, dir, ".env", "SECRET=1\n")

	p, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Repo != "git@github.com:someone/thing.git" {
		t.Errorf("Repo = %q", p.Repo)
	}
	if p.Branch != "main" {
		t.Errorf("Branch = %q", p.Branch)
	}
	if !p.HasEnv {
		t.Error("the .env should be noticed")
	}
	// This is the check worth having: a secret file that git would commit.
	if !p.EnvIgnored {
		t.Error(".env is in .gitignore and should be reported as ignored")
	}
}

func TestUnignoredEnvIsReported(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	write(t, dir, ".env", "SECRET=1\n")

	p, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.EnvIgnored {
		t.Error("with no .gitignore rule the .env is not ignored, and that is worth warning about")
	}
}
