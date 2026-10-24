package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSaveEnvKeepsCommentsAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, `# the app
APP_NAME=old

# the database
export DB_HOST=localhost
DB_PASSWORD=
`)

	err := SaveEnv(path, []Entry{
		{Key: "APP_NAME", Value: "demo"},
		{Key: "DB_PASSWORD", Value: "s3cret"},
		{Key: "REDIS_HOST", Value: "redis"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	want := `# the app
APP_NAME=demo

# the database
export DB_HOST=localhost
DB_PASSWORD=s3cret

# Added by hploy.
REDIS_HOST=redis
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSaveEnvLeavesUnmentionedKeysAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, "KEEP=mine\nCHANGE=old\n")

	if err := SaveEnv(path, []Entry{{Key: "CHANGE", Value: "new"}}); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "KEEP=mine\nCHANGE=new\n" {
		t.Errorf("got %q", got)
	}
}

func TestSaveEnvCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if err := SaveEnv(path, []Entry{{Key: "A", Value: "1"}}); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "# Written by hploy.") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(string(got), "A=1\n") {
		t.Errorf("got %q", got)
	}

	// The file holds credentials, so it must not be world readable.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

func TestSaveEnvDoesNotGrowBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, "A=1\n")

	for i := 0; i < 3; i++ {
		if err := SaveEnv(path, []Entry{{Key: "A", Value: "1"}}); err != nil {
			t.Fatal(err)
		}
	}

	got, _ := os.ReadFile(path)
	if string(got) != "A=1\n" {
		t.Errorf("got %q", got)
	}
}

func TestSaveEnvRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	// Every one of these has broken a naive env writer at some point.
	entries := []Entry{
		{Key: "SPACES", Value: "two words"},
		{Key: "HASH", Value: "not#a#comment"},
		{Key: "DOLLAR", Value: "pa$$word"},
		{Key: "QUOTE", Value: `say "hi"`},
		{Key: "APOSTROPHE", Value: "it's"},
		{Key: "EQUALS", Value: "a=b=c"},
		{Key: "EMPTY", Value: ""},
		{Key: "LOOKS_QUOTED", Value: `"literal"`},
	}
	if err := SaveEnv(path, entries); err != nil {
		t.Fatal(err)
	}

	back, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if back[e.Key] != e.Value {
			t.Errorf("%s = %q, want %q", e.Key, back[e.Key], e.Value)
		}
	}
}

func TestEnvOrderFollowsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, "# comment\nZ=1\n\nexport A=2\nZ=duplicate\nnot a pair\n")

	got, err := EnvOrder(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Z" || got[1] != "A" {
		t.Errorf("got %v", got)
	}
}

func TestEnvOrderOnMissingFile(t *testing.T) {
	got, err := EnvOrder(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatalf("a missing env file is not an error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v", got)
	}
}

// A key on two lines happens: someone pastes a block, or an older hploy
// appended what was already above. LoadFile is last-wins, so updating only
// the first occurrence reported a save and handed back the old value.
func TestSaveEnvUpdatesEveryLineOfAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, "DB_PASSWORD=old\nOTHER=keep\nDB_PASSWORD=old\n")

	if err := SaveEnv(path, []Entry{{Key: "DB_PASSWORD", Value: "new"}}); err != nil {
		t.Fatal(err)
	}

	back, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if back["DB_PASSWORD"] != "new" {
		t.Errorf("reads back %q, want the value that was saved", back["DB_PASSWORD"])
	}
	if back["OTHER"] != "keep" {
		t.Errorf("OTHER = %q", back["OTHER"])
	}
	// Nothing is deleted: the file is the application's, and dropping a line
	// is not this function's call to make.
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "DB_PASSWORD=") != 2 {
		t.Errorf("lines changed:\n%s", raw)
	}
	if strings.Contains(string(raw), "=old") {
		t.Errorf("an old value survived:\n%s", raw)
	}
}
