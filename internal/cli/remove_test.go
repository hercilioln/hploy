package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/store"
)

// The one property this command must never break: the clone is the user's,
// and its .env exists nowhere else because git ignores it.
func TestUnregisterLeavesTheCloneAlone(t *testing.T) {
	t.Setenv("HPLOY_HOME", t.TempDir())
	clone := t.TempDir()
	if err := os.WriteFile(filepath.Join(clone, ".env"), []byte("DB_PASSWORD=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &store.App{Name: "demo", Path: clone}
	if err := os.MkdirAll(store.AppDir("demo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(app); err != nil {
		t.Fatal(err)
	}

	if err := unregister(app); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(store.AppDir("demo")); !os.IsNotExist(err) {
		t.Error("the store entry should be gone")
	}
	if _, err := os.Stat(filepath.Join(clone, ".env")); err != nil {
		t.Errorf("the clone's .env was touched: %v", err)
	}
	if _, err := store.LoadApp("demo"); err == nil {
		t.Error("the app should no longer be registered")
	}
}

// Unregistering a running app would leave containers labelled for a name
// hploy no longer knows, which `down` could not reach any more.
func TestRemoveRefusesWhileSomethingIsRunning(t *testing.T) {
	err := stillRunning("demo",
		[]engine.Container{{Name: "demo-web"}, {Name: "demo-db"}}, "hploy_demo")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"2 container(s)", "a network", "hploy down demo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, err)
		}
	}
}

func TestInStoreTellsAGeneratedSpecFromACommittedOne(t *testing.T) {
	t.Setenv("HPLOY_HOME", t.TempDir())
	clone := t.TempDir()

	generated := &store.App{Name: "demo", Path: clone}
	if !inStore(generated) {
		t.Error("with no hploy.yaml in the clone the spec is the store's copy")
	}

	if err := os.WriteFile(filepath.Join(clone, "hploy.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if inStore(generated) {
		t.Error("a committed spec wins, so removing the app loses nothing")
	}
}
