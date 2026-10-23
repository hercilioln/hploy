package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanDomain(t *testing.T) {
	ok := map[string]string{
		"nagarrafa.com":     "nagarrafa.com",
		"  NaGarrafa.com ":  "nagarrafa.com",
		"www.nagarrafa.com": "www.nagarrafa.com",
		"example.localhost": "example.localhost",
		"localhost":         "localhost",
		"a-b.co.uk":         "a-b.co.uk",
	}
	for in, want := range ok {
		got, err := CleanDomain(in)
		if err != nil {
			t.Errorf("CleanDomain(%q) failed: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("CleanDomain(%q) = %q, want %q", in, got, want)
		}
	}

	// Each message must name the mistake: this is what the wizard shows when
	// someone pastes from the address bar.
	bad := map[string]string{
		"https://nagarrafa.com": "scheme",
		"nagarrafa.com/app":     "path",
		"nagarrafa.com:8080":    "port",
		"-nagarrafa.com":        "valid hostname",
		"na garrafa.com":        "valid hostname",
		"":                      "empty",
	}
	for in, wantMsg := range bad {
		_, err := CleanDomain(in)
		if err == nil {
			t.Errorf("CleanDomain(%q) should have failed", in)
			continue
		}
		if !strings.Contains(err.Error(), wantMsg) {
			t.Errorf("CleanDomain(%q) said %q, want it to mention %q", in, err, wantMsg)
		}
	}
}

func TestSaveAndLoadApp(t *testing.T) {
	t.Setenv("HPLOY_HOME", t.TempDir())

	in := &App{
		Name:    "nagarrafa",
		Domains: []string{"NaGarrafa.com", "www.nagarrafa.com"},
		Path:    "/home/deploy/apps/nagarrafa",
		Repo:    "git@github.com:hercilioln/nagarrafa.git",
		Branch:  "main",
	}
	if err := SaveApp(in); err != nil {
		t.Fatal(err)
	}

	out, err := LoadApp("nagarrafa")
	if err != nil {
		t.Fatal(err)
	}
	if out.Domains[0] != "nagarrafa.com" {
		t.Errorf("the domain should have been normalized, got %q", out.Domains[0])
	}
	if out.Path != in.Path || out.Branch != "main" {
		t.Errorf("round trip lost data: %+v", out)
	}
}

func TestUnregisteredAppSaysWhatToDo(t *testing.T) {
	t.Setenv("HPLOY_HOME", t.TempDir())
	_, err := LoadApp("ghost")
	if err == nil {
		t.Fatal("loading an unregistered app should fail")
	}
	if !strings.Contains(err.Error(), "hploy add") {
		t.Errorf("the error should point at the fix, got %q", err)
	}
}

func TestValidateRejectsBadConfig(t *testing.T) {
	cases := map[string]*App{
		"relative path":  {Name: "x", Path: "apps/x"},
		"bad name":       {Name: "Not Valid", Path: "/srv/x"},
		"duplicate host": {Name: "x", Path: "/srv/x", Domains: []string{"a.com", "A.com"}},
	}
	for name, a := range cases {
		if err := a.Validate(); err == nil {
			t.Errorf("%s: should have been rejected", name)
		}
	}
}

// The spec in the repo wins, so a git pull is what changes the deployment.
func TestSpecPathPrefersTheRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HPLOY_HOME", home)
	clone := t.TempDir()

	a := &App{Name: "demo", Path: clone}

	fallback := filepath.Join(AppDir("demo"), "hploy.yaml")
	if got := a.SpecPath(); got != fallback {
		t.Errorf("with no spec in the clone, want the store copy %q, got %q", fallback, got)
	}

	inRepo := filepath.Join(clone, "hploy.yaml")
	if err := os.WriteFile(inRepo, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.SpecPath(); got != inRepo {
		t.Errorf("the committed spec should win, got %q", got)
	}
}

func TestListAppsSkipsFoldersWithoutConfig(t *testing.T) {
	t.Setenv("HPLOY_HOME", t.TempDir())

	if err := SaveApp(&App{Name: "beta", Path: "/srv/beta"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveApp(&App{Name: "alpha", Path: "/srv/alpha"}); err != nil {
		t.Fatal(err)
	}
	// A folder holding only secrets is not a registered app.
	if err := os.MkdirAll(AppDir("leftover"), 0o700); err != nil {
		t.Fatal(err)
	}

	apps, err := ListApps()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2", len(apps))
	}
	if apps[0].Name != "alpha" || apps[1].Name != "beta" {
		t.Errorf("apps should be sorted, got %q then %q", apps[0].Name, apps[1].Name)
	}
}

func TestUnknownFieldIsAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HPLOY_HOME", home)
	if err := os.MkdirAll(AppDir("demo"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "app: demo\npath: /srv/demo\ndomian: typo.com\n"
	if err := os.WriteFile(AppConfigPath("demo"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadApp("demo"); err == nil {
		t.Error("a misspelled key should be an error, not a silent default")
	}
}
