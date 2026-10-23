package buildctx

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"Dockerfile":            "FROM scratch\n",
		".dockerignore":         "node_modules\nstorage\n.env\n!.env.example\nDockerfile\n",
		".env":                  "SECRET=1\n",
		".env.example":          "SECRET=\n",
		"app/main.go":           "package main\n",
		"node_modules/dep/i.js": "module.exports={}\n",
		"storage/logs/big.log":  "aaaa\n",
		"storage/app/photo.png": "PNG\n",
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func names(t *testing.T, r io.Reader) []string {
	t.Helper()
	var out []string
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, h.Name)
		io.Copy(io.Discard, tr)
	}
	sort.Strings(out)
	return out
}

func TestTarRespectsDockerignore(t *testing.T) {
	dir := fixture(t)
	var buf bytes.Buffer
	if _, err := Tar(dir, "Dockerfile", &buf); err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, n := range names(t, &buf) {
		got[n] = true
	}

	// The Dockerfile is excluded by .dockerignore and must be sent anyway:
	// without it the daemon has nothing to build.
	for _, want := range []string{"Dockerfile", ".dockerignore", ".env.example", "app/", "app/main.go"} {
		if !got[want] {
			t.Errorf("%q should be in the context", want)
		}
	}
	for _, unwanted := range []string{".env", "node_modules/", "node_modules/dep/i.js", "storage/app/photo.png"} {
		if got[unwanted] {
			t.Errorf("%q must not reach the daemon", unwanted)
		}
	}
}

// Measure is what gets printed before a build, so it has to agree with what
// is actually sent.
func TestMeasureMatchesTar(t *testing.T) {
	dir := fixture(t)

	measured, err := Measure(dir, "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tarred, err := Tar(dir, "Dockerfile", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if measured != tarred {
		t.Errorf("Measure said %+v, Tar sent %+v", measured, tarred)
	}
	if measured.Files == 0 {
		t.Error("the context should not be empty")
	}
}

func TestExcludedDirectoryIsNotWalked(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte("skipme\n"), 0o644)
	deep := filepath.Join(dir, "skipme", "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(deep, "file.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644)

	var buf bytes.Buffer
	st, err := Tar(dir, "Dockerfile", &buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names(t, &buf) {
		if n != "Dockerfile" && n != ".dockerignore" {
			t.Errorf("unexpected entry %q", n)
		}
	}
	if st.Files != 2 {
		t.Errorf("files = %d, want 2", st.Files)
	}
}

func TestSymlinkIsSentAsALink(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "real.txt"), []byte("hello"), 0o644)
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skip("symlinks unavailable")
	}

	var buf bytes.Buffer
	if _, err := Tar(dir, "Dockerfile", &buf); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == "link.txt" {
			if h.Typeflag != tar.TypeSymlink || h.Linkname != "real.txt" {
				t.Errorf("link.txt should be a symlink to real.txt, got type %v -> %q", h.Typeflag, h.Linkname)
			}
			return
		}
		io.Copy(io.Discard, tr)
	}
	t.Error("link.txt is missing from the context")
}

func TestCleanDockerfileStaysInsideTheContext(t *testing.T) {
	if got, _ := CleanDockerfile(""); got != "Dockerfile" {
		t.Errorf("default = %q", got)
	}
	if got, _ := CleanDockerfile("docker/prod.Dockerfile"); got != "docker/prod.Dockerfile" {
		t.Errorf("got %q", got)
	}
	for _, bad := range []string{"../outside", "/etc/passwd", "a/../../b"} {
		if _, err := CleanDockerfile(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
