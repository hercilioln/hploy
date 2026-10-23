package spec

import (
	"strings"
	"testing"
)

func TestRepoExampleIsValid(t *testing.T) {
	s, err := Load("../../example/hploy.yaml")
	if err != nil {
		t.Fatalf("the example shipped in the repo must not fail: %v", err)
	}
	if s.App != "example" {
		t.Errorf("app = %q, want \"example\"", s.App)
	}
	if s.Web == nil || s.Web.Port != 80 {
		t.Error("web should exist with port 80")
	}
	// The "health: /" shorthand must become a Health with Path plus defaults.
	if s.Web.Health == nil || s.Web.Health.Path != "/" {
		t.Errorf("health shorthand did not become a Path: %+v", s.Web.Health)
	}
	if s.Web.Health.Retries != 5 {
		t.Errorf("default retries = %d, want 5", s.Web.Health.Retries)
	}
	if got := s.Services["db"].Health.Cmd[0]; got != "pg_isready" {
		t.Errorf("db health.cmd = %q", got)
	}
	if s.Services["db"].Resources.Memory != 128<<20 {
		t.Errorf("db memory = %d", s.Services["db"].Resources.Memory)
	}
	// Start order must leave web last.
	order := s.StartOrder()
	if order[len(order)-1] != s.Web {
		t.Error("web should start last, after the database")
	}
}

func TestValidationCatchesMistakes(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "support service publishing a port",
			yaml: `
version: 1
app: test
services:
  db:
    image: postgres:17-alpine
    port: 5432
    resources: { memory: 64m }
`,
			want: "port is not allowed on a support service",
		},
		{
			name: "volume used without being declared",
			yaml: `
version: 1
app: test
services:
  db:
    image: postgres:17-alpine
    volumes: ["ghost:/data"]
    resources: { memory: 64m }
`,
			want: "not declared in the volumes block",
		},
		{
			name: "no memory limit",
			yaml: `
version: 1
app: test
services:
  db:
    image: postgres:17-alpine
`,
			want: "resources.memory is required",
		},
		{
			name: "web without a domain",
			yaml: `
version: 1
app: test
web:
  image: nginx:alpine
  port: 80
  resources: { memory: 64m }
`,
			want: "domains is required",
		},
		{
			name: "unknown field (typo)",
			yaml: `
version: 1
app: test
web:
  image: nginx:alpine
  port: 80
  resorces: { memory: 64m }
`,
			want: "field resorces not found",
		},
		{
			name: "invalid app name",
			yaml: "version: 1\napp: Example_With_Caps\n",
			want: "is invalid",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml))
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error was %q, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestHashIsStableAndSensitive(t *testing.T) {
	s, err := Load("../../example/hploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	a := s.Web.Hash(s.App, nil)
	if b := s.Web.Hash(s.App, nil); a != b {
		t.Error("the hash must be stable across calls")
	}

	// A secret value must NOT enter the hash: changing a password should not
	// recreate the container.
	if c := s.Web.Hash(s.App, map[string]string{"SECRET": "other"}); a != c {
		t.Error("an env value should not change the hash")
	}

	// But changing the image must change it.
	s.Web.Image = "nginx:1.28-alpine"
	if s.Web.Hash(s.App, nil) == a {
		t.Error("changing the image should have changed the hash")
	}
}
