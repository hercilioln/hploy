package plan

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hercilioln/hploy/internal/buildctx"
	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/spec"
)

// Writer is the write half of the engine, separate from engine.Engine so a
// read-only command cannot reach these by accident.
type Writer interface {
	engine.Engine
	CreateNetwork(ctx context.Context, name string, labels map[string]string) error
	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	Create(ctx context.Context, cs engine.ContainerSpec) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string, timeout time.Duration) error
	Remove(ctx context.Context, id string, force bool) error
	PullImage(ctx context.Context, image string) error
	Inspect(ctx context.Context, id string) (engine.Inspected, error)
	Build(ctx context.Context, dir string, opt engine.BuildOptions, out io.Writer) (string, error)
}

// Output is where Execute reports. A build has its own stream, which does not
// fit a one-line-per-step callback.
type Output struct {
	Step Progress
	Logs io.Writer // nil discards the build output
}

func (o Output) step(verb Verb, kind Kind, name, note string) {
	if o.Step != nil {
		o.Step(verb, kind, name, note)
	}
}

func (o Output) logs() io.Writer {
	if o.Logs == nil {
		return io.Discard
	}
	return o.Logs
}

// Progress reports each step as it happens, so a slow apply is not a silent
// wait.
type Progress func(verb Verb, kind Kind, name, note string)

// Execute carries out a plan. It refuses to run one with problems: a container
// started without its secrets is worse than one not started.
func Execute(ctx context.Context, w Writer, t *Target, p *Plan, out Output) error {
	sp := t.Spec
	if len(p.Problems) > 0 {
		return fmt.Errorf("the plan has %d unresolved problem(s)", len(p.Problems))
	}

	labels := func(service string) map[string]string {
		l := map[string]string{engine.LabelApp: sp.App}
		if service != "" {
			l[engine.LabelService] = service
		}
		return l
	}

	for _, a := range p.Actions {
		if a.Verb == Unchanged {
			continue
		}
		switch a.Kind {
		case KindNetwork:
			out.step(a.Verb, a.Kind, a.Name, "")
			if err := w.CreateNetwork(ctx, a.Name, labels("")); err != nil {
				return fmt.Errorf("network %s: %w", a.Name, err)
			}

		case KindVolume:
			out.step(a.Verb, a.Kind, a.Name, "")
			if err := w.CreateVolume(ctx, a.Name, labels("")); err != nil {
				return fmt.Errorf("volume %s: %w", a.Name, err)
			}

		case KindImage:
			if err := applyImage(ctx, w, t, a, out); err != nil {
				return fmt.Errorf("image %s: %w", a.Name, err)
			}

		case KindContainer:
			if err := applyContainer(ctx, w, t, a, out); err != nil {
				return fmt.Errorf("container %s: %w", a.Name, err)
			}
		}
	}
	return nil
}

// applyImage builds or pulls, whichever the service asked for.
func applyImage(ctx context.Context, w Writer, t *Target, a Action, out Output) error {
	sv := serviceByImage(t.Spec, a.Name)
	if sv == nil || sv.Build == nil {
		out.step(a.Verb, a.Kind, a.Name, "pulling")
		return w.PullImage(ctx, a.Name)
	}
	return BuildService(ctx, w, t, sv, engine.BuildOptions{}, out)
}

// BuildService makes one service's image. Exported because `hploy build` runs
// it without a plan.
func BuildService(ctx context.Context, w Writer, t *Target, sv *spec.Service, opt engine.BuildOptions, out Output) error {
	dir := filepath.Join(t.Dir, sv.Build.ContextOrDefault())

	dockerfile, err := buildctx.CleanDockerfile(sv.Build.Dockerfile)
	if err != nil {
		return err
	}
	st, err := buildctx.Measure(dir, dockerfile)
	if err != nil {
		return fmt.Errorf("reading the build context %s: %w", dir, err)
	}

	args, err := resolveArgs(sv.Build.Args, t.Env)
	if err != nil {
		return err
	}

	opt.Tag = sv.Image
	opt.Dockerfile = dockerfile
	opt.Target = sv.Build.Target
	opt.Args = args
	opt.Labels = map[string]string{engine.LabelApp: t.Spec.App, engine.LabelService: sv.Name()}

	out.step(Create, KindImage, sv.Image,
		fmt.Sprintf("context %s: %d files, %s", sv.Build.ContextOrDefault(), st.Files, humanBytes(st.Bytes)))

	id, err := w.Build(ctx, dir, opt, out.logs())
	if err != nil {
		return err
	}
	if id != "" {
		out.step(Create, KindImage, sv.Image, "built "+shortID(id))
	}
	return nil
}

// resolveArgs lets a build arg read from the app env, the same ${NAME} rule
// the service env uses.
func resolveArgs(args, env map[string]string) (map[string]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	return spec.Env{Vars: args}.Resolve(env)
}

func humanBytes(n int64) string {
	const unit = 1000.0
	f := float64(n)
	if f < unit {
		return fmt.Sprintf("%dB", n)
	}
	for _, suffix := range []string{"kB", "MB", "GB", "TB"} {
		f /= unit
		if f < unit {
			return fmt.Sprintf("%.1f%s", f, suffix)
		}
	}
	return fmt.Sprintf("%.0fTB", f)
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func applyContainer(ctx context.Context, w Writer, t *Target, a Action, out Output) error {
	sp := t.Spec

	// Remove needs no spec: the service is gone from the file.
	if a.Verb == Remove {
		out.step(a.Verb, a.Kind, a.Name, "")
		if err := w.Stop(ctx, a.Name, 10*time.Second); err != nil {
			return err
		}
		return w.Remove(ctx, a.Name, false)
	}

	sv := serviceFor(sp, a.Name)
	if sv == nil {
		return fmt.Errorf("no service in the spec matches this container")
	}

	if a.Verb == Start {
		out.step(a.Verb, a.Kind, a.Name, "")
		if err := w.Start(ctx, a.Name); err != nil {
			return err
		}
		return waitHealthy(ctx, w, sp, sv, a.Name, out)
	}

	if a.Verb == Recreate {
		out.step(a.Verb, a.Kind, a.Name, "stopping the old one")
		if err := w.Stop(ctx, a.Name, 10*time.Second); err != nil {
			return err
		}
		if err := w.Remove(ctx, a.Name, false); err != nil {
			return err
		}
	}

	// The plan creates a missing image on its own; this covers the case where
	// it vanished in between, so a create never fails for a missing image.
	id, err := w.ImageID(ctx, sv.Image)
	if err != nil {
		return err
	}
	if id == "" {
		if sv.Build != nil {
			if err := BuildService(ctx, w, t, sv, engine.BuildOptions{}, out); err != nil {
				return err
			}
		} else {
			out.step(a.Verb, a.Kind, a.Name, "pulling "+sv.Image)
			if err := w.PullImage(ctx, sv.Image); err != nil {
				return err
			}
		}
	}

	cs, err := containerSpec(t, sv)
	if err != nil {
		return err
	}
	out.step(a.Verb, a.Kind, a.Name, "")
	if _, err := w.Create(ctx, cs); err != nil {
		return err
	}
	if err := w.Start(ctx, a.Name); err != nil {
		return err
	}
	return waitHealthy(ctx, w, sp, sv, a.Name, out)
}

func containerSpec(t *Target, sv *spec.Service) (engine.ContainerSpec, error) {
	sp := t.Spec
	resolved, err := sv.Env.Resolve(t.Env)
	if err != nil {
		return engine.ContainerSpec{}, err
	}

	mounts := make([]engine.Mount, 0, len(sv.Volumes))
	for _, v := range sv.Volumes {
		name, target, _ := strings.Cut(v, ":")
		mounts = append(mounts, engine.Mount{Volume: VolumeName(sp.App, name), Target: target})
	}

	cs := engine.ContainerSpec{
		Name:    ContainerName(sp.App, sv.Name()),
		Image:   sv.Image,
		Cmd:     sv.Command,
		Env:     resolved,
		Network: NetworkName(sp.App),
		// The alias is the service name, so the app reaches its database at
		// "db" rather than "example-db".
		Aliases: []string{sv.Name()},
		Mounts:  mounts,
		Memory:  uint64(sv.Resources.Memory),
		Labels: map[string]string{
			engine.LabelApp:      sp.App,
			engine.LabelService:  sv.Name(),
			engine.LabelSpecHash: sv.Hash(sp.App),
		},
	}

	if sv.Resources.CPUs != "" {
		cpus, err := strconv.ParseFloat(sv.Resources.CPUs, 64)
		if err != nil {
			return cs, fmt.Errorf("resources.cpus %q: %w", sv.Resources.CPUs, err)
		}
		cs.NanoCPUs = int64(cpus * 1e9)
	}

	// Only a command probe becomes a Docker healthcheck; an HTTP path is
	// probed by hploy, since the image may carry no HTTP client.
	if sv.Health != nil && len(sv.Health.Cmd) > 0 {
		cs.Health = &engine.HealthCheck{
			Cmd:      sv.Health.Cmd,
			Interval: sv.Health.Interval,
			Timeout:  sv.Health.Timeout,
			Retries:  sv.Health.Retries,
		}
	}
	return cs, nil
}

// waitHealthy blocks until the service answers, so the next one starts against
// a working dependency.
func waitHealthy(ctx context.Context, w Writer, sp *spec.Spec, sv *spec.Service, name string, out Output) error {
	if sv.Health == nil {
		return nil
	}
	deadline := time.Now().Add(time.Duration(sv.Health.Retries) * sv.Health.Interval)
	if time.Until(deadline) < 10*time.Second {
		deadline = time.Now().Add(10 * time.Second)
	}

	out.step(Unchanged, KindContainer, name, "waiting for "+sv.Health.String())
	for {
		insp, err := w.Inspect(ctx, name)
		if err != nil {
			return err
		}
		if insp.State != "running" {
			return fmt.Errorf("stopped while starting (state %s)", insp.State)
		}

		ok, err := probe(ctx, sv, insp)
		if err == nil && ok {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("never became healthy: %w", err)
			}
			return fmt.Errorf("never became healthy")
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func probe(ctx context.Context, sv *spec.Service, insp engine.Inspected) (bool, error) {
	// Command probes are Docker's job; we only read the verdict.
	if len(sv.Health.Cmd) > 0 {
		switch insp.Health {
		case "healthy":
			return true, nil
		case "unhealthy":
			return false, fmt.Errorf("healthcheck reports unhealthy")
		default:
			return false, nil
		}
	}

	if insp.IP == "" {
		return false, nil
	}
	url := fmt.Sprintf("http://%s%s", net.JoinHostPort(insp.IP, strconv.Itoa(sv.Port)), sv.Health.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := (&http.Client{Timeout: sv.Health.Timeout}).Do(req)
	if err != nil {
		return false, nil // not up yet
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, nil
	}
	return false, fmt.Errorf("%s returned %d", sv.Health.Path, resp.StatusCode)
}

func serviceByImage(sp *spec.Spec, image string) *spec.Service {
	for _, sv := range sp.StartOrder() {
		if sv.Image == image {
			return sv
		}
	}
	return nil
}

func serviceFor(sp *spec.Spec, containerName string) *spec.Service {
	for _, sv := range sp.StartOrder() {
		if ContainerName(sp.App, sv.Name()) == containerName {
			return sv
		}
	}
	return nil
}
