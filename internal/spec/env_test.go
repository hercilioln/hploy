package spec

import "testing"

func fromFileService(t *testing.T) *Service {
	t.Helper()
	sp, err := Parse([]byte(`
version: 1
app: demo
web:
  image: demo:latest
  port: 80
  resources: { memory: 64m }
  env:
    from_file: true
    vars:
      APP_ENV: production
`))
	if err != nil {
		t.Fatal(err)
	}
	return sp.Web
}

func TestFromFileInjectsTheWholeFile(t *testing.T) {
	sv := fromFileService(t)
	fileEnv := map[string]string{"DB_HOST": "db", "APP_KEY": "base64:abc", "APP_ENV": "local"}

	got, err := sv.Env.Resolve(fileEnv, fileEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got["DB_HOST"] != "db" || got["APP_KEY"] != "base64:abc" {
		t.Errorf("the file should have been injected, got %v", got)
	}
	// An explicit var is the stronger statement.
	if got["APP_ENV"] != "production" {
		t.Errorf("APP_ENV = %q, want the spec's value to win", got["APP_ENV"])
	}
}

// hploy's own store can hold a webhook or registry secret. from_file injects
// the application's file only, so those never reach the container.
func TestFromFileDoesNotLeakHployOwnSecrets(t *testing.T) {
	sv := fromFileService(t)
	fileEnv := map[string]string{"DB_HOST": "db"}
	lookup := map[string]string{"DB_HOST": "db", "HPLOY_WEBHOOK_SECRET": "whsec_do_not_leak"}

	got, err := sv.Env.Resolve(lookup, fileEnv)
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := got["HPLOY_WEBHOOK_SECRET"]; leaked {
		t.Error("a store-only secret must not be injected into the app container")
	}
	if got["DB_HOST"] != "db" {
		t.Error("the application's own file should still be injected")
	}
}

func TestTheEnvironmentIsInTheHash(t *testing.T) {
	sv := fromFileService(t)
	injected := func(fileEnv map[string]string) string {
		env, err := sv.Env.Resolve(fileEnv, fileEnv)
		if err != nil {
			t.Fatal(err)
		}
		return sv.Hash("demo", env)
	}

	two := map[string]string{"A": "1", "B": "2"}
	before := injected(two)

	if injected(map[string]string{"A": "1", "B": "2", "C": "3"}) == before {
		t.Error("a new variable has to recreate the container, or it would run without it")
	}

	// This reverses an earlier decision, which was that rotating a value
	// should not recreate the container. A container's environment is fixed
	// when it is created, so leaving it running meant the new value never
	// reached the application -- and `hploy env` told people to apply.
	if injected(map[string]string{"A": "changed", "B": "2"}) == before {
		t.Error("a changed value has to recreate the container, or it never takes effect")
	}

	// Order in the file is not configuration.
	if injected(map[string]string{"B": "2", "A": "1"}) != before {
		t.Error("the same variables in another order must hash the same")
	}
}

// A service without from_file ignores the file entirely, so a database is
// never handed the application's secrets.
func TestWithoutFromFileNothingIsInjected(t *testing.T) {
	sp, err := Parse([]byte(`
version: 1
app: demo
web:
  image: demo:latest
  port: 80
  resources: { memory: 64m }
services:
  db:
    image: postgres:17-alpine
    resources: { memory: 64m }
    env:
      vars:
        POSTGRES_PASSWORD: ${DB_PASSWORD}
`))
	if err != nil {
		t.Fatal(err)
	}
	db := sp.Services["db"]
	fileEnv := map[string]string{"DB_PASSWORD": "s3cr3t", "STRIPE_KEY": "sk_live_x"}

	got, err := db.Env.Resolve(fileEnv, fileEnv)
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := got["STRIPE_KEY"]; leaked {
		t.Error("the database must not receive the application's environment")
	}
	if got["POSTGRES_PASSWORD"] != "s3cr3t" {
		t.Errorf("the declared var should resolve, got %q", got["POSTGRES_PASSWORD"])
	}
	if len(got) != 1 {
		t.Errorf("only the declared var belongs here, got %v", got)
	}
	// A service without from_file hashes only what it declares, so another
	// service's variable cannot recreate it.
	env, err := db.Env.Resolve(fileEnv, fileEnv)
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := env["APP_KEY"]; leaked {
		t.Error("a database must not be handed the application's own variables")
	}
}
