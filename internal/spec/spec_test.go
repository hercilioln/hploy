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
	if got := s.Services["db"].Env.Vars["POSTGRES_PASSWORD"]; got != "${DB_PASSWORD}" {
		t.Errorf("the template must be kept raw, got %q", got)
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
	a := s.Web.Hash(s.App)
	if b := s.Web.Hash(s.App); a != b {
		t.Error("the hash must be stable across calls")
	}

	// Changing the image must change it.
	s.Web.Image = "nginx:1.28-alpine"
	if s.Web.Hash(s.App) == a {
		t.Error("changing the image should have changed the hash")
	}

	// Rotating a secret must NOT: the hash sees the template, not the value.
	db := s.Services["db"]
	before := db.Hash(s.App)
	one, err := db.Env.Resolve(map[string]string{
		"DB_DATABASE": "example", "DB_USERNAME": "example", "DB_PASSWORD": "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	two, err := db.Env.Resolve(map[string]string{
		"DB_DATABASE": "example", "DB_USERNAME": "example", "DB_PASSWORD": "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if one["POSTGRES_PASSWORD"] == two["POSTGRES_PASSWORD"] {
		t.Error("resolution should reflect the new value")
	}
	if db.Hash(s.App) != before {
		t.Error("rotating a secret must not change the hash")
	}
}

func TestInterpolation(t *testing.T) {
	sp, err := Load("../../example/hploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	db := sp.Services["db"]

	appEnv := map[string]string{
		"DB_DATABASE": "example",
		"DB_USERNAME": "example",
		// A dollar sign in a password must survive untouched: only ${NAME}
		// interpolates, a bare $ is literal.
		"DB_PASSWORD": "p$N9S-x",
	}
	got, err := db.Env.Resolve(appEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got["POSTGRES_PASSWORD"] != "p$N9S-x" {
		t.Errorf("password came out as %q", got["POSTGRES_PASSWORD"])
	}
	if got["POSTGRES_DB"] != "example" {
		t.Errorf("POSTGRES_DB = %q", got["POSTGRES_DB"])
	}

	// A literal passes through unchanged.
	if v := sp.Web.Env.Vars["NGINX_ENTRYPOINT_QUIET_LOGS"]; v != "1" {
		t.Errorf("literal = %q", v)
	}
}

// An unknown reference must fail loudly. Substituting an empty string would
// start Postgres with a blank password and say nothing.
func TestUnknownReferenceIsAnError(t *testing.T) {
	sp, err := Load("../../example/hploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sp.Services["db"].Env.Resolve(map[string]string{"DB_DATABASE": "x"})
	if err == nil {
		t.Fatal("expected an error for the unset references")
	}
	for _, want := range []string{"DB_USERNAME", "DB_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}
}

func TestMissingListsReferencedAndRequired(t *testing.T) {
	sp, err := Parse([]byte(`
version: 1
app: test
services:
  app:
    image: alpine
    resources: { memory: 64m }
    env:
      required: [APP_KEY]
      vars:
        PASS: ${DB_PASSWORD}
`))
	if err != nil {
		t.Fatal(err)
	}
	missing := sp.Services["app"].Env.Missing(map[string]string{})
	if len(missing) != 2 || missing[0] != "APP_KEY" || missing[1] != "DB_PASSWORD" {
		t.Errorf("missing = %v, want [APP_KEY DB_PASSWORD]", missing)
	}
	if left := sp.Services["app"].Env.Missing(map[string]string{
		"APP_KEY": "k", "DB_PASSWORD": "p",
	}); len(left) != 0 {
		t.Errorf("nothing should be missing, got %v", left)
	}
}

func TestEnvFileMustStayInsideTheApp(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../other/.env"} {
		_, err := Parse([]byte("version: 1\napp: test\nenv_file: " + bad +
			"\nservices:\n  a:\n    image: alpine\n    resources: { memory: 64m }\n"))
		if err == nil || !strings.Contains(err.Error(), "relative path inside the app") {
			t.Errorf("env_file %q should be rejected, got %v", bad, err)
		}
	}
}
