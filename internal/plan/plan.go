package plan

import (
	"context"
	"fmt"
	"sort"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/spec"
)

type Verb string

const (
	Create    Verb = "create"
	Recreate  Verb = "recreate"
	Start     Verb = "start"
	Remove    Verb = "remove"
	Unchanged Verb = "unchanged"
)

type Kind string

const (
	KindNetwork   Kind = "network"
	KindVolume    Kind = "volume"
	KindImage     Kind = "image"
	KindContainer Kind = "container"
)

type Action struct {
	Verb   Verb
	Kind   Kind
	Name   string // the Docker object name
	Detail string // image, or whatever helps identify it
	Reason string // why, when it is not obvious
}

// Target is an app resolved to everything needed to act on it: the spec from
// the repository and the deployment config from the store.
type Target struct {
	Spec *spec.Spec

	// Env is the ${NAME} lookup; FileEnv is the application's own env file,
	// which is all that env.from_file injects. See store.AppEnv.
	Env     map[string]string
	FileEnv map[string]string

	// Dir is the spec's directory: build contexts resolve against it.
	Dir     string
	Domains []string

	// Registered says the app came from the store, so it gets a domain and a
	// proxy route. A bare directory is a local test and wants neither.
	Registered bool
}

func (t *Target) App() string { return t.Spec.App }

// Hash fingerprints a service in this target, including the names the env
// file would inject.
func (t *Target) Hash(sv *spec.Service) string {
	// A reference that does not resolve is reported as a problem and blocks
	// apply, so the hash of that state is never written to a container. It
	// still must not collide with the resolved one.
	env, err := sv.Env.Resolve(t.Env, t.FileEnv)
	if err != nil {
		return sv.Hash(t.Spec.App, nil)
	}
	return sv.Hash(t.Spec.App, env)
}

type Plan struct {
	App     string
	Actions []Action

	// Problems block execution: missing secrets, mostly. Collected here so
	// one run reports all of them.
	Problems []string

	// Warnings do not block. Something is worth saying, but refusing to
	// deploy over it would be worse than deploying.
	Warnings []string
}

func (p *Plan) Count(v Verb) int {
	n := 0
	for _, a := range p.Actions {
		if a.Verb == v {
			n++
		}
	}
	return n
}

// Empty reports whether there is nothing to do.
func (p *Plan) Empty() bool {
	for _, a := range p.Actions {
		if a.Verb != Unchanged {
			return false
		}
	}
	return true
}

func (p *Plan) add(v Verb, k Kind, name, detail, reason string) {
	p.Actions = append(p.Actions, Action{Verb: v, Kind: k, Name: name, Detail: detail, Reason: reason})
}

// Build diffs the target against the machine.
func Build(ctx context.Context, eng engine.Engine, t *Target) (*Plan, error) {
	sp := t.Spec
	p := &Plan{App: sp.App}

	networks, err := eng.ListNetworks(ctx)
	if err != nil {
		return nil, err
	}
	volumes, err := eng.ListVolumes(ctx)
	if err != nil {
		return nil, err
	}
	// all=true: a stopped container still occupies the name, so ignoring it
	// would plan a create that then fails.
	containers, err := eng.List(ctx, true)
	if err != nil {
		return nil, err
	}

	// The image a tag points at is observed state, like a container: a rebuild
	// moves it without the spec changing at all.
	images := map[string]string{}
	for _, sv := range sp.StartOrder() {
		if _, done := images[sv.Image]; done {
			continue
		}
		id, err := eng.ImageID(ctx, sv.Image)
		if err != nil {
			return nil, err
		}
		images[sv.Image] = id
	}

	p.planNetwork(sp, networks)
	p.planVolumes(sp, volumes)
	p.planImages(sp, images)
	p.planContainers(t, containers, images)
	p.checkEnv(sp, t.Env, t.FileEnv)
	p.checkRouting(t)
	return p, nil
}

func (p *Plan) planNetwork(sp *spec.Spec, existing []engine.Network) {
	name := NetworkName(sp.App)
	for _, n := range existing {
		if n.Name == name {
			p.add(Unchanged, KindNetwork, name, "", "")
			return
		}
	}
	p.add(Create, KindNetwork, name, "", "")
}

func (p *Plan) planVolumes(sp *spec.Spec, existing []engine.Volume) {
	have := map[string]bool{}
	for _, v := range existing {
		have[v.Name] = true
	}

	declared := make([]string, 0, len(sp.Volumes))
	for name := range sp.Volumes {
		declared = append(declared, name)
	}
	sort.Strings(declared)

	for _, short := range declared {
		full := VolumeName(sp.App, short)
		if have[full] {
			p.add(Unchanged, KindVolume, full, "", "")
			continue
		}
		p.add(Create, KindVolume, full, "", "")
	}

	// A volume no longer declared is never removed automatically: it holds
	// data, and the spec is not authoritative enough to justify deleting it.
	for _, v := range existing {
		if v.App != sp.App {
			continue
		}
		stillUsed := false
		for _, short := range declared {
			if VolumeName(sp.App, short) == v.Name {
				stillUsed = true
				break
			}
		}
		if !stillUsed {
			// A warning, not a problem: leftover data is untidy, but blocking
			// every future deploy over it is worse.
			p.Warnings = append(p.Warnings,
				fmt.Sprintf("volume %s belongs to this app but is no longer declared; remove it by hand if the data is not needed", v.Name))
		}
	}
}

// planImages reports the images that are not on the machine yet. A build or a
// pull takes minutes, so it belongs in the plan rather than surprising
// someone mid-apply.
//
// An image that is already local is left alone: rebuilding is `hploy build`,
// which then makes the container stale through its new ID.
func (p *Plan) planImages(sp *spec.Spec, images map[string]string) {
	seen := map[string]bool{}
	for _, sv := range sp.StartOrder() {
		if seen[sv.Image] || images[sv.Image] != "" {
			continue
		}
		seen[sv.Image] = true

		if sv.Build != nil {
			detail := "build " + sv.Build.ContextOrDefault()
			if sv.Build.Dockerfile != "" {
				detail += "/" + sv.Build.Dockerfile
			}
			p.add(Create, KindImage, sv.Image, detail, "")
			continue
		}
		p.add(Create, KindImage, sv.Image, "pull", "")
	}
}

func (p *Plan) planContainers(t *Target, existing []engine.Container, images map[string]string) {
	sp := t.Spec

	// Only containers carrying our label are considered. Anything else on the
	// machine is invisible to hploy.
	ours := map[string]engine.Container{}
	for _, c := range existing {
		if c.App == sp.App && c.Service != "" {
			ours[c.Service] = c
		}
	}

	for _, sv := range sp.StartOrder() {
		name := ContainerName(sp.App, sv.Name())
		want := t.Hash(sv)
		current, exists := ours[sv.Name()]
		delete(ours, sv.Name())

		// An unknown ID means the image is not local; that is the pull's
		// problem, not a reason to recreate.
		wantImage := images[sv.Image]
		staleImage := wantImage != "" && current.ImageID != "" && current.ImageID != wantImage

		switch {
		case !exists:
			p.add(Create, KindContainer, name, sv.Image, "")
		case current.SpecHash != want:
			p.add(Recreate, KindContainer, name, sv.Image, "config changed")
		case staleImage:
			p.add(Recreate, KindContainer, name, sv.Image, "new image")
		case current.State != "running":
			p.add(Start, KindContainer, name, sv.Image, "state is "+current.State)
		default:
			p.add(Unchanged, KindContainer, name, sv.Image, "")
		}
	}

	// Whatever is left carries our label but is not in the spec any more.
	leftover := make([]string, 0, len(ours))
	for service := range ours {
		leftover = append(leftover, service)
	}
	sort.Strings(leftover)
	for _, service := range leftover {
		c := ours[service]
		p.add(Remove, KindContainer, c.Name, c.Image, "not in the spec")
	}
}

// checkRouting reports an app that will come up but receive no traffic.
func (p *Plan) checkRouting(t *Target) {
	if !t.Registered || t.Spec.Web == nil {
		return
	}
	if len(t.Domains) == 0 {
		p.Warnings = append(p.Warnings,
			"no domain is set for this app, so the proxy will not route to it")
	}
}

func (p *Plan) checkEnv(sp *spec.Spec, env, fileEnv map[string]string) {
	seen := map[string]bool{}
	var missing []string
	for _, sv := range sp.StartOrder() {
		for _, key := range sv.Env.Missing(env) {
			if !seen[key] {
				seen[key] = true
				missing = append(missing, key)
			}
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		p.Problems = append(p.Problems,
			fmt.Sprintf("%s is referenced by the spec but not set in %s", key, sp.EnvFileOrDefault()))
	}

	// A service asking for the whole file and getting nothing is almost
	// always an env file that was never filled in. It is not a Problem --
	// hploy cannot know which variables an application truly needs -- but
	// silence here means a crash loop that plan said nothing about.
	for _, sv := range sp.StartOrder() {
		if sv.Env.FromFile && len(fileEnv) == 0 {
			p.Warnings = append(p.Warnings, fmt.Sprintf(
				"%s injects the whole %s, which has no variables — run `hploy env`",
				sv.Name(), sp.EnvFileOrDefault()))
		}
	}
}
