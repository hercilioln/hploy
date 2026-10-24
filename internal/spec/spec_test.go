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
	a := s.Web.Hash(s.App, nil)
	if b := s.Web.Hash(s.App, nil); a != b {
		t.Error("the hash must be stable across calls")
	}

	// Changing the image must change it.
	s.Web.Image = "nginx:1.28-alpine"
	if s.Web.Hash(s.App, nil) == a {
		t.Error("changing the image should have changed the hash")
	}

	// Rotating a secret must NOT: the hash sees the template, not the value.
	db := s.Services["db"]
	before := db.Hash(s.App, nil)
	one, err := db.Env.Resolve(map[string]string{
		"DB_DATABASE": "example", "DB_USERNAME": "example", "DB_PASSWORD": "first",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := db.Env.Resolve(map[string]string{
		"DB_DATABASE": "example", "DB_USERNAME": "example", "DB_PASSWORD": "second",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if one["POSTGRES_PASSWORD"] == two["POSTGRES_PASSWORD"] {
		t.Error("resolution should reflect the new value")
	}
	if db.Hash(s.App, nil) != before {
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
	got, err := db.Env.Resolve(appEnv, nil)
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
	_, err = sp.Services["db"].Env.Resolve(map[string]string{"DB_DATABASE": "x"}, nil)
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

// tcp is the shape that cannot be wrong about the protocol, so it is the one
// a generated spec reaches for first.
func TestHealthTCPShorthand(t *testing.T) {
	sp, err := Parse([]byte(`version: 1
app: demo
web:
  image: nginx:1.29-alpine
  port: 9000
  health: tcp
  resources: {memory: 128m}
`))
	if err != nil {
		t.Fatal(err)
	}
	h := sp.Web.Health
	if h == nil || !h.TCP {
		t.Fatalf("health = %#v", h)
	}
	if h.Path != "" || len(h.Cmd) > 0 {
		t.Errorf("tcp must not also carry a path or a command: %#v", h)
	}
	if h.Interval == 0 || h.Timeout == 0 || h.Retries == 0 {
		t.Errorf("the defaults were not applied: %#v", h)
	}
	if got := h.String(); got == "none" || got == "" {
		t.Errorf("String() = %q, and the hash depends on it", got)
	}
}

func TestHealthShapesAreExclusive(t *testing.T) {
	for _, bad := range []string{
		"  health: {tcp: true, path: /up}",
		"  health: {tcp: true, cmd: [\"true\"]}",
		"  health: {path: /up, cmd: [\"true\"]}",
		"  health: {interval: 5s}",
		"  health: up",
	} {
		_, err := Parse([]byte("version: 1\napp: demo\nweb:\n  image: nginx\n  port: 80\n" +
			bad + "\n  resources: {memory: 128m}\n"))
		if err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// tcp and a path both probe the port, so a service without one cannot use
// them. A support service has no port by design.
func TestHealthNeedsAPortToProbe(t *testing.T) {
	_, err := Parse([]byte(`version: 1
app: demo
web:
  image: nginx:1.29-alpine
  port: 80
  resources: {memory: 128m}
services:
  worker:
    image: demo-worker:latest
    health: tcp
    resources: {memory: 128m}
`))
	if err == nil {
		t.Fatal("a service with no port accepted a tcp healthcheck")
	}
	if !strings.Contains(err.Error(), "needs a port") {
		t.Errorf("err = %v", err)
	}

	// A command needs nothing, so it is still allowed there.
	if _, err := Parse([]byte(`version: 1
app: demo
web:
  image: nginx:1.29-alpine
  port: 80
  resources: {memory: 128m}
services:
  worker:
    image: demo-worker:latest
    health: {cmd: ["pgrep", "worker"]}
    resources: {memory: 128m}
`)); err != nil {
		t.Errorf("a command healthcheck needs no port: %v", err)
	}
}

// Docker refuses anything under 6MB at container create -- after the image
// was built and the network and volumes were made. Catching it in the spec
// turns that into a plan error.
func TestMemoryBelowDockersFloorIsRejected(t *testing.T) {
	withMemory := func(m string) error {
		_, err := Parse([]byte(`version: 1
app: demo
web:
  image: nginx:1.29-alpine
  port: 80
  resources: {memory: ` + m + `}
`))
		return err
	}

	// The mistake that produced this test: a bare number is bytes, so "256"
	// is 256 bytes and not the 256 megabytes whoever typed it meant.
	err := withMemory("256")
	if err == nil {
		t.Fatal("256 bytes was accepted")
	}
	if !strings.Contains(err.Error(), "did you mean 256m?") {
		t.Errorf("the error does not name the mistake:\n%v", err)
	}

	for _, tiny := range []string{"5", "1m", "0.5m", "6291455"} {
		if withMemory(tiny) == nil {
			t.Errorf("%s was accepted, and Docker would refuse it", tiny)
		}
	}
	for _, ok := range []string{"6m", "64m", "512m", "2g", "6291456"} {
		if err := withMemory(ok); err != nil {
			t.Errorf("%s was rejected: %v", ok, err)
		}
	}

	// A guess only helps when it is plausible: 6291455 bytes is not 6291455
	// megabytes.
	if err := withMemory("6291455"); strings.Contains(err.Error(), "did you mean") {
		t.Errorf("a nonsense suggestion:\n%v", err)
	}
}
