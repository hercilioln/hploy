// Package spec reads and validates hploy.yaml: the desired state of an app,
// with no knowledge of Docker.
//
// The spec describes the application and nothing about where it runs. Domain,
// secrets and clone path are deployment config and live in the store, so this
// file can be committed to the app's repository.
package spec

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Spec is a whole hploy.yaml.
type Spec struct {
	Version int    `yaml:"version"`
	App     string `yaml:"app"`

	// EnvFile is the application's env file, relative to the spec. It is the
	// single source of truth for secrets: the app reads it directly and hploy
	// only interpolates ${NAME} from it.
	EnvFile string `yaml:"env_file"`

	Web      *Service            `yaml:"web"`
	Services map[string]*Service `yaml:"services"`
	Volumes  map[string]Volume   `yaml:"volumes"`

	// Source is the file the spec came from.
	Source string `yaml:"-"`
}

// Service is one container.
type Service struct {
	// Image is the tag to run. With a build block it is the tag produced,
	// defaulting to "<app>-<service>:latest".
	Image     string    `yaml:"image"`
	Build     *Build    `yaml:"build"`
	Command   []string  `yaml:"command"`
	Port      int       `yaml:"port"`
	Health    *Health   `yaml:"health"`
	Env       Env       `yaml:"env"`
	Volumes   []string  `yaml:"volumes"` // "name:/path/in/container"
	Resources Resources `yaml:"resources"`

	// name is filled in while loading; it does not come from the file.
	name string `yaml:"-"`
}

func (s *Service) Name() string { return s.name }

// Build makes the image from a Dockerfile instead of pulling it. This is what
// lets hploy deploy any application that ships one.
type Build struct {
	// Context is the directory sent to the daemon, relative to the spec.
	Context    string            `yaml:"context"`
	Dockerfile string            `yaml:"dockerfile"`
	Target     string            `yaml:"target"` // multi-stage stage
	Args       map[string]string `yaml:"args"`
}

// ContextOrDefault is the build context, defaulting to the spec's directory.
func (b *Build) ContextOrDefault() string {
	if b.Context == "" {
		return "."
	}
	return b.Context
}

// Volume is declared so plan knows which volumes to create.
type Volume struct {
	// reserved for driver/opts
}

// Resources caps the container. A missing memory limit is a validation error,
// not a warning.
type Resources struct {
	Memory Bytes  `yaml:"memory"`
	CPUs   string `yaml:"cpus"`
}

var reName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

// Load reads and validates an hploy.yaml.
func Load(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.Source = path
	return s, nil
}

// Validate reports every problem at once rather than stopping at the first.
func (s *Spec) Validate() error {
	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if s.Version != 1 {
		fail("version must be 1 (got %d)", s.Version)
	}
	if s.App == "" {
		fail("app is required")
	} else if !reName.MatchString(s.App) {
		fail("app %q is invalid: use lowercase, digits and hyphen, starting and ending alphanumeric", s.App)
	}
	if s.Web == nil && len(s.Services) == 0 {
		fail("the spec declares no services: web or services is missing")
	}

	for _, sv := range s.all() {
		prefix := "service " + sv.Name()
		if !reName.MatchString(sv.Name()) {
			fail("%s: invalid name", prefix)
		}
		if sv.Image == "" {
			fail("%s: set image, or build to make one", prefix)
		}
		if sv.Build != nil {
			ctxDir := sv.Build.ContextOrDefault()
			if strings.HasPrefix(ctxDir, "/") || strings.Contains(ctxDir, "..") {
				fail("%s: build.context must be a relative path inside the app directory", prefix)
			}
			if df := sv.Build.Dockerfile; strings.HasPrefix(df, "/") || strings.Contains(df, "..") {
				fail("%s: build.dockerfile must be inside the build context", prefix)
			}
		}
		if sv.Resources.Memory == 0 {
			fail("%s: resources.memory is required", prefix)
		}
		for _, v := range sv.Volumes {
			name, path, ok := strings.Cut(v, ":")
			switch {
			case !ok:
				fail("%s: volume %q must be \"name:/path\"", prefix, v)
			case !strings.HasPrefix(path, "/"):
				fail("%s: volume %q: the path must be absolute", prefix, v)
			case !reName.MatchString(name):
				fail("%s: volume %q: invalid name", prefix, name)
			default:
				if _, declared := s.Volumes[name]; !declared {
					fail("%s: volume %q is used but not declared in the volumes block", prefix, name)
				}
			}
		}
	}

	if strings.HasPrefix(s.EnvFile, "/") || strings.Contains(s.EnvFile, "..") {
		fail("env_file must be a relative path inside the app directory")
	}

	if s.Web != nil && s.Web.Port == 0 {
		fail("web: port is required — it is how the proxy finds the app")
	}

	for name, sv := range s.Services {
		if sv.Port != 0 {
			fail("services.%s: port is not allowed on a support service — use the internal network", name)
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("invalid spec:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// all returns web + services in a stable order.
func (s *Spec) all() []*Service {
	var out []*Service
	if s.Web != nil {
		out = append(out, s.Web)
	}
	names := make([]string, 0, len(s.Services))
	for n := range s.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, s.Services[n])
	}
	return out
}

// StartOrder puts web last, so its dependencies come up first.
func (s *Spec) StartOrder() []*Service {
	all := s.all()
	out := make([]*Service, 0, len(all))
	for _, sv := range all {
		if sv != s.Web {
			out = append(out, sv)
		}
	}
	if s.Web != nil {
		out = append(out, s.Web)
	}
	return out
}

// EnvFileOrDefault is the app env file, defaulting to .env next to the spec.
func (s *Spec) EnvFileOrDefault() string {
	if s.EnvFile != "" {
		return s.EnvFile
	}
	return ".env"
}
