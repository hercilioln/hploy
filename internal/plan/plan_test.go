package plan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/spec"
)

// fakeEngine is why engine.Engine is an interface: the diff logic is tested
// without a daemon.
type fakeEngine struct {
	containers []engine.Container
	networks   []engine.Network
	volumes    []engine.Volume
	images     map[string]string // reference -> current ID
	logs       string            // what Logs hands back
}

func (f *fakeEngine) Ping(context.Context) (string, error) { return "fake", nil }
func (f *fakeEngine) List(context.Context, bool) ([]engine.Container, error) {
	return f.containers, nil
}
func (f *fakeEngine) ListNetworks(context.Context) ([]engine.Network, error) {
	return f.networks, nil
}
func (f *fakeEngine) ListVolumes(context.Context) ([]engine.Volume, error) {
	return f.volumes, nil
}
func (f *fakeEngine) ImageID(_ context.Context, ref string) (string, error) {
	return f.images[ref], nil
}
func (f *fakeEngine) Stats(context.Context, []string) (<-chan engine.Sample, <-chan error) {
	return nil, nil
}
func (f *fakeEngine) Logs(context.Context, string, engine.LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func load(t *testing.T) *spec.Spec {
	t.Helper()
	sp, err := spec.Load("../../example/hploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func exampleEnv() map[string]string {
	return map[string]string{"DB_DATABASE": "example", "DB_USERNAME": "example", "DB_PASSWORD": "x"}
}

func target(sp *spec.Spec, env map[string]string) *Target {
	return &Target{Spec: sp, Env: env}
}

// hashOf is the label an already-converged container would carry. It goes
// through the Target so the test cannot drift from what apply writes --
// which is the whole reason the hash exists.
func hashOf(sp *spec.Spec, env map[string]string, sv *spec.Service) string {
	return target(sp, env).Hash(sv)
}

// present marks every image of a spec as already on the machine, which is the
// normal state once an app has been deployed once.
func present(sp *spec.Spec) map[string]string {
	out := map[string]string{}
	for _, sv := range sp.StartOrder() {
		out[sv.Image] = "sha256:" + sv.Name()
	}
	return out
}

func verbOf(p *Plan, name string) Verb {
	for _, a := range p.Actions {
		if a.Name == name {
			return a.Verb
		}
	}
	return ""
}

func TestEmptyMachinePlansEverything(t *testing.T) {
	sp := load(t)
	p, err := Build(context.Background(), &fakeEngine{}, target(sp, exampleEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Count(Create); got != 6 {
		t.Errorf("creates = %d, want 6 (network, volume, 2 images, 2 containers)", got)
	}
	// A pull takes minutes, so it is planned rather than sprung mid-apply.
	if verbOf(p, "nginx:1.29-alpine") != Create {
		t.Error("the missing image should be planned")
	}
	if verbOf(p, "hploy_example") != Create {
		t.Error("the network should be created")
	}
	if verbOf(p, "hploy_example_data") != Create {
		t.Error("the volume should be created")
	}
	if verbOf(p, "example-db") != Create || verbOf(p, "example-web") != Create {
		t.Error("both containers should be created")
	}
	if len(p.Problems) != 0 {
		t.Errorf("no problems expected, got %v", p.Problems)
	}
}

// The whole point of the hash: applying twice must be a no-op.
func TestConvergedMachinePlansNothing(t *testing.T) {
	sp := load(t)
	env := exampleEnv()

	f := &fakeEngine{
		networks: []engine.Network{{Name: "hploy_example", App: "example"}},
		volumes:  []engine.Volume{{Name: "hploy_example_data", App: "example"}},
		images:   present(sp),
	}
	for _, sv := range sp.StartOrder() {
		f.containers = append(f.containers, engine.Container{
			Name:     ContainerName(sp.App, sv.Name()),
			App:      sp.App,
			Service:  sv.Name(),
			ImageID:  f.images[sv.Image],
			SpecHash: hashOf(sp, exampleEnv(), sv),
			State:    "running",
		})
	}

	p, err := Build(context.Background(), f, target(sp, env))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() {
		t.Errorf("a converged machine should plan nothing, got %+v", p.Actions)
	}
}

func TestChangedConfigPlansRecreate(t *testing.T) {
	sp := load(t)
	env := exampleEnv()

	f := &fakeEngine{
		networks: []engine.Network{{Name: "hploy_example", App: "example"}},
		volumes:  []engine.Volume{{Name: "hploy_example_data", App: "example"}},
		images:   present(sp),
	}
	f.containers = []engine.Container{
		{Name: "example-web", App: "example", Service: "web",
			ImageID: f.images[sp.Web.Image], SpecHash: "stale", State: "running"},
		{Name: "example-db", App: "example", Service: "db",
			ImageID:  f.images[sp.Services["db"].Image],
			SpecHash: hashOf(sp, env, sp.Services["db"]), State: "running"},
	}

	p, err := Build(context.Background(), f, target(sp, env))
	if err != nil {
		t.Fatal(err)
	}
	if verbOf(p, "example-web") != Recreate {
		t.Errorf("web = %q, want recreate", verbOf(p, "example-web"))
	}
	if verbOf(p, "example-db") != Unchanged {
		t.Errorf("db = %q, want unchanged", verbOf(p, "example-db"))
	}
}

func TestStoppedContainerPlansStart(t *testing.T) {
	sp := load(t)
	env := exampleEnv()

	f := &fakeEngine{
		networks: []engine.Network{{Name: "hploy_example", App: "example"}},
		volumes:  []engine.Volume{{Name: "hploy_example_data", App: "example"}},
		images:   present(sp),
	}
	for _, sv := range sp.StartOrder() {
		state := "running"
		if sv.Name() == "db" {
			state = "exited"
		}
		f.containers = append(f.containers, engine.Container{
			Name:     ContainerName(sp.App, sv.Name()),
			App:      sp.App,
			Service:  sv.Name(),
			ImageID:  f.images[sv.Image],
			SpecHash: hashOf(sp, exampleEnv(), sv),
			State:    state,
		})
	}

	p, err := Build(context.Background(), f, target(sp, env))
	if err != nil {
		t.Fatal(err)
	}
	if verbOf(p, "example-db") != Start {
		t.Errorf("a stopped container should be started, got %q", verbOf(p, "example-db"))
	}
}

// The safety invariant: anything without our label is invisible.
func TestForeignContainersAreIgnored(t *testing.T) {
	sp := load(t)
	f := &fakeEngine{
		containers: []engine.Container{
			{Name: "wordpress_carumemoria", Image: "wp", State: "running"},
			{Name: "nginx-proxy", Image: "nginxproxy", State: "running"},
			{Name: "example-db", App: "other-app", Service: "db", State: "running"},
		},
	}
	p, err := Build(context.Background(), f, target(sp, exampleEnv()))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Actions {
		if a.Verb == Remove {
			t.Errorf("planned to remove %q, which is not ours", a.Name)
		}
	}
}

func TestOrphanServicePlansRemove(t *testing.T) {
	sp := load(t)
	f := &fakeEngine{
		containers: []engine.Container{
			{Name: "example-cache", App: "example", Service: "cache",
				Image: "redis:alpine", State: "running"},
		},
	}
	p, err := Build(context.Background(), f, target(sp, exampleEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if verbOf(p, "example-cache") != Remove {
		t.Error("a labelled container absent from the spec should be removed")
	}
}

func TestMissingSecretBlocks(t *testing.T) {
	sp := load(t)
	p, err := Build(context.Background(), &fakeEngine{}, target(sp, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) == 0 {
		t.Fatal("a missing required secret should be reported")
	}
	if !strings.Contains(p.Problems[0], "DB_DATABASE") {
		t.Errorf("problem was %q", p.Problems[0])
	}
}

// A leftover volume used to land in Problems, which blocked every later apply.
// It is untidy, not dangerous.
func TestLeftoverVolumeWarnsButDoesNotBlock(t *testing.T) {
	sp := load(t)
	f := &fakeEngine{
		images: present(sp),
		volumes: []engine.Volume{
			{Name: "hploy_example_data", App: "example"},
			{Name: "hploy_example_old", App: "example"},
		},
	}
	p, err := Build(context.Background(), f, target(sp, exampleEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) != 0 {
		t.Errorf("should not block, got problems %v", p.Problems)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "hploy_example_old") {
		t.Errorf("want one warning naming the volume, got %v", p.Warnings)
	}
	for _, a := range p.Actions {
		if a.Verb == Remove {
			t.Errorf("a volume must never be removed automatically: %q", a.Name)
		}
	}
}

func TestRegisteredAppWithoutDomainWarns(t *testing.T) {
	sp := load(t)

	t1 := target(sp, exampleEnv())
	t1.Registered = true
	p, err := Build(context.Background(), &fakeEngine{}, t1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "domain") {
		t.Errorf("a registered web app with no domain should warn, got %v", p.Warnings)
	}

	// A bare directory is a local test: no domain expected, no noise.
	p, err = Build(context.Background(), &fakeEngine{}, target(sp, exampleEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Warnings) != 0 {
		t.Errorf("an unregistered target should not warn, got %v", p.Warnings)
	}
}

// A rebuilt tag leaves the spec untouched, so the hash cannot see it. Without
// the image comparison the new build would never run.
func TestRebuiltImagePlansRecreate(t *testing.T) {
	sp := load(t)
	f := &fakeEngine{
		networks: []engine.Network{{Name: "hploy_example", App: "example"}},
		volumes:  []engine.Volume{{Name: "hploy_example_data", App: "example"}},
		images: map[string]string{
			"nginx:1.29-alpine":  "sha256:new",
			"postgres:17-alpine": "sha256:db",
		},
	}
	for _, sv := range sp.StartOrder() {
		id := "sha256:db"
		if sv.Name() == "web" {
			id = "sha256:old" // the tag has moved since this container was created
		}
		f.containers = append(f.containers, engine.Container{
			Name:     ContainerName(sp.App, sv.Name()),
			App:      sp.App,
			Service:  sv.Name(),
			ImageID:  id,
			SpecHash: hashOf(sp, exampleEnv(), sv),
			State:    "running",
		})
	}

	p, err := Build(context.Background(), f, target(sp, exampleEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if verbOf(p, "example-web") != Recreate {
		t.Errorf("web = %q, want recreate on a new image", verbOf(p, "example-web"))
	}
	if verbOf(p, "example-db") != Unchanged {
		t.Errorf("db = %q, want unchanged: its image did not move", verbOf(p, "example-db"))
	}
}

// An image that is not local must not make a running container look stale:
// churn here would restart a healthy app for nothing.
func TestAbsentImageDoesNotPlanRecreate(t *testing.T) {
	sp := load(t)
	f := &fakeEngine{
		networks: []engine.Network{{Name: "hploy_example", App: "example"}},
		volumes:  []engine.Volume{{Name: "hploy_example_data", App: "example"}},
	}
	for _, sv := range sp.StartOrder() {
		f.containers = append(f.containers, engine.Container{
			Name:     ContainerName(sp.App, sv.Name()),
			App:      sp.App,
			Service:  sv.Name(),
			ImageID:  "sha256:whatever",
			SpecHash: hashOf(sp, exampleEnv(), sv),
			State:    "running",
		})
	}
	p, err := Build(context.Background(), f, target(sp, exampleEnv()))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"example-web", "example-db"} {
		if got := verbOf(p, name); got != Unchanged {
			t.Errorf("%s = %q, want unchanged", name, got)
		}
	}
}

// The failure that started this: php-fpm answers on 9000 in FastCGI, the HTTP
// probe never succeeds, and the container's own log says "ready to handle
// connections". Without the reason, the error and the log contradict
// each other and neither is wrong.
func TestProbeHintExplainsANonHTTPPort(t *testing.T) {
	sv := &spec.Service{Port: 9000, Health: &spec.Health{Path: "/"}}
	err := fmt.Errorf(`GET / on port 9000: malformed HTTP response "\x01\x06\x00\x01"`)

	hint := probeHint(err, sv)
	if hint == "" {
		t.Fatal("a port answering in something other than HTTP needs explaining")
	}
	if !strings.Contains(hint, "not in HTTP") {
		t.Errorf("hint = %q", hint)
	}
	// It has to say what to do, not only what happened.
	if !strings.Contains(hint, "cmd:") {
		t.Errorf("the hint names no way out:\n%s", hint)
	}
}

func TestProbeHintExplainsAClosedPort(t *testing.T) {
	sv := &spec.Service{Port: 8080, Health: &spec.Health{Path: "/"}}
	err := fmt.Errorf("GET / on port 8080: %w", syscall.ECONNREFUSED)

	hint := probeHint(err, sv)
	if !strings.Contains(hint, "nothing is listening on port 8080") {
		t.Errorf("hint = %q", hint)
	}
}

// Still booting is the common case and must stay quiet.
func TestProbeHintSaysNothingAboutATimeout(t *testing.T) {
	sv := &spec.Service{Port: 8080, Health: &spec.Health{Path: "/"}}
	if hint := probeHint(fmt.Errorf("context deadline exceeded"), sv); hint != "" {
		t.Errorf("hint = %q, want none", hint)
	}
}

func TestUnwrapURLDropsTheRepeatedPrefix(t *testing.T) {
	wrapped := &url.Error{Op: "Get", URL: "http://172.17.0.2:9000/", Err: syscall.ECONNREFUSED}

	got := unwrapURL(wrapped)
	if !errors.Is(got, syscall.ECONNREFUSED) {
		t.Fatalf("got %v", got)
	}
	if strings.Contains(got.Error(), "http://") {
		t.Errorf("the URL is already in the message: %q", got)
	}
}

// A container that failed its healthcheck having logged nothing is exactly
// when the tail matters, and strings.Split never returns an empty slice -- so
// counting lines is the only way to know the log was silent.
func TestContainerTailSaysSoWhenTheLogIsEmpty(t *testing.T) {
	for _, logged := range []string{"", "\n", "   \n\n"} {
		f := &fakeEngine{logs: logged}
		got := containerTail(context.Background(), f, "demo-web")
		if !strings.Contains(got, "printed nothing at all") {
			t.Errorf("for %q got %q", logged, got)
		}
		if strings.Contains(got, "last lines of") {
			t.Errorf("for %q it promised lines it does not have: %q", logged, got)
		}
	}

	f := &fakeEngine{logs: "boot failed\n"}
	got := containerTail(context.Background(), f, "demo-web")
	if !strings.Contains(got, "last lines of demo-web") || !strings.Contains(got, "boot failed") {
		t.Errorf("got %q", got)
	}
}
