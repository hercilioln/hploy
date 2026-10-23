// Package spec reads and validates hploy.yaml.
//
// The spec describes the DESIRED state of an app. It knows nothing about
// Docker -- translating it into containers, networks and volumes is the plan
// package's job. That separation is what lets us validate a spec with no
// daemon anywhere in sight.
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
	Version  int                 `yaml:"version"`
	App      string              `yaml:"app"`
	Domains  []string            `yaml:"domains"`
	Web      *Service            `yaml:"web"`
	Services map[string]*Service `yaml:"services"`
	Volumes  map[string]Volume   `yaml:"volumes"`

	// Source records which file the spec came from, so error messages can
	// point at the right place.
	Source string `yaml:"-"`
}

// Service is one container.
type Service struct {
	Image     string    `yaml:"image"`
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

// Volume has no options yet; it exists so the spec declares which volumes the
// app uses and plan can create the missing ones.
type Volume struct {
	// reserved for driver/opts later
}

// Env declares WHICH variables the service needs -- never the values.
//
// A secret in a version-controlled file is a leak waiting to happen. Values
// come from ~/.hploy/apps/<app>/env, which never reaches git.
type Env struct {
	Required []string          `yaml:"required"`
	Optional []string          `yaml:"optional"`
	Literal  map[string]string `yaml:"literal"` // non-secret values only
}

// Resources caps the container.
//
// On a 4 GB VPS running two apps, a memory limit is the difference between
// "one app dies" and "the machine starts swapping and everything goes down
// together". That is why a missing limit is an error, not a warning.
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

// Validate collects EVERY problem before returning, instead of stopping at
// the first one. Whoever is writing the file would rather see the whole list.
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
			fail("%s: image is required", prefix)
		}
		if sv.Resources.Memory == 0 {
			fail("%s: resources.memory is not set — with no limit, one app alone can take the machine down", prefix)
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

	if s.Web != nil {
		if s.Web.Port == 0 {
			fail("web: port is required — it is how the proxy finds the app")
		}
		if len(s.Domains) == 0 {
			fail("domains is required when there is a web service")
		}
	} else if len(s.Domains) > 0 {
		fail("domains is declared but there is no web service to serve them")
	}

	// A support service with a port is the mistake that exposes a database to
	// the internet.
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

// StartOrder returns the services with web last.
//
// Order matters on apply: the database has to be up before web, otherwise the
// application starts unable to connect.
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

// EnvNames lists every variable the service expects.
func (sv *Service) EnvNames() []string {
	out := append([]string{}, sv.Env.Required...)
	out = append(out, sv.Env.Optional...)
	for k := range sv.Env.Literal {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
