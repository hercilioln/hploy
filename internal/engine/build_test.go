package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Build needs a real daemon, so it is opt-in:
//
//	HPLOY_TEST_DOCKER=1 go test ./internal/engine/
func requireDocker(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("HPLOY_TEST_DOCKER") == "" {
		t.Skip("set HPLOY_TEST_DOCKER=1 to run against the local daemon")
	}
	c := New("")
	if _, err := c.Ping(context.Background()); err != nil {
		t.Skipf("no daemon: %v", err)
	}
	return c
}

func TestBuildAgainstRealDaemon(t *testing.T) {
	c := requireDocker(t)
	ctx := context.Background()

	dir := t.TempDir()
	write := func(name, content string) {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile", "FROM alpine:3.21\nARG GREETING=hello\nRUN echo \"$GREETING\" > /greeting\nCOPY payload.txt /payload.txt\n")
	write("payload.txt", "in the context\n")
	write("node_modules/junk.js", "should not be sent\n")
	write(".dockerignore", "node_modules\n")

	var log strings.Builder
	id, err := c.Build(ctx, dir, BuildOptions{
		Tag:    "hploy-selftest:latest",
		Args:   map[string]string{"GREETING": "built by hploy"},
		Labels: map[string]string{LabelApp: "selftest"},
	}, &log)
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, log.String())
	}
	t.Cleanup(func() {
		resp, err := c.send(ctx, "DELETE", "/images/hploy-selftest:latest"+query("force", "1"), nil)
		if err == nil {
			resp.Body.Close()
		}
	})

	if id == "" {
		t.Error("the build reported no image ID")
	}
	out := log.String()
	if !strings.Contains(out, "Step 1/") {
		t.Errorf("the build log looks empty:\n%s", out)
	}

	// ImageID must agree with what the build returned: this pair is what
	// plan uses to notice a rebuild.
	resolved, err := c.ImageID(ctx, "hploy-selftest:latest")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != id {
		t.Errorf("ImageID = %q, build said %q", resolved, id)
	}

	// An absent image is an empty string, not an error.
	missing, err := c.ImageID(ctx, "hploy-definitely-absent:latest")
	if err != nil || missing != "" {
		t.Errorf("absent image gave (%q, %v), want (\"\", nil)", missing, err)
	}
}

func TestBuildFailureSurfacesTheError(t *testing.T) {
	c := requireDocker(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"),
		[]byte("FROM alpine:3.21\nRUN exit 7\n"), 0o644)

	_, err := c.Build(context.Background(), dir, BuildOptions{Tag: "hploy-selftest-fail:latest"}, io.Discard)
	if err == nil {
		t.Fatal("a failing RUN should be an error")
	}
	// The daemon reports it inside a 200 response, so it has to be read from
	// the stream rather than the status code.
	if !strings.Contains(err.Error(), "build failed") {
		t.Errorf("error was %q", err)
	}
}
