package spec

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Parse reads the YAML and fills in what does not come from the file.
func Parse(b []byte) (*Spec, error) {
	var s Spec
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	// KnownFields turns an unknown key into an error instead of silently
	// ignoring it. A typo in a field name is the nastiest config bug there is:
	// everything "works", except the option was never applied.
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}

	if s.Web != nil {
		s.Web.name = "web"
	}
	for name, sv := range s.Services {
		if sv == nil {
			return nil, fmt.Errorf("services.%s: empty block", name)
		}
		sv.name = name
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Bytes accepts "512m", "2g", "268435456".
type Bytes uint64

func (b *Bytes) UnmarshalYAML(n *yaml.Node) error {
	var raw string
	if err := n.Decode(&raw); err != nil {
		// A bare number is fine too.
		var v uint64
		if err2 := n.Decode(&v); err2 != nil {
			return fmt.Errorf("memory: expected a string like \"512m\" or a number")
		}
		*b = Bytes(v)
		return nil
	}

	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return nil
	}
	mult := uint64(1)
	switch {
	case strings.HasSuffix(raw, "k"), strings.HasSuffix(raw, "kb"):
		mult = 1 << 10
	case strings.HasSuffix(raw, "m"), strings.HasSuffix(raw, "mb"):
		mult = 1 << 20
	case strings.HasSuffix(raw, "g"), strings.HasSuffix(raw, "gb"):
		mult = 1 << 30
	}
	num := strings.TrimRight(raw, "kmgb")
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return fmt.Errorf("memory %q: cannot parse", raw)
	}
	if v < 0 {
		return fmt.Errorf("memory %q: cannot be negative", raw)
	}
	*b = Bytes(v * float64(mult))
	return nil
}

func (b Bytes) String() string {
	switch {
	case b == 0:
		return "no limit"
	case b >= 1<<30:
		return fmt.Sprintf("%.3gG", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.3gM", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%dB", uint64(b))
	}
}

// Health accepts two shapes, because a web service and a support service need
// different probes:
//
//	health: /up                                -> HTTP GET on that path
//	health: { cmd: ["pg_isready", "-U", "x"] } -> command inside the container
type Health struct {
	Path     string        `yaml:"path"`
	Cmd      []string      `yaml:"cmd"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Retries  int           `yaml:"retries"`
}

func (h *Health) UnmarshalYAML(n *yaml.Node) error {
	// Shorthand: health: /up
	if n.Kind == yaml.ScalarNode {
		var path string
		if err := n.Decode(&path); err != nil {
			return err
		}
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("health %q: the shorthand must be a path starting with /", path)
		}
		h.Path = path
		h.applyDefaults()
		return nil
	}

	// Full form. A helper type so we do not recurse into our own Unmarshal.
	type raw Health
	var r raw
	if err := n.Decode(&r); err != nil {
		return err
	}
	*h = Health(r)
	if h.Path == "" && len(h.Cmd) == 0 {
		return fmt.Errorf("health: set either path or cmd")
	}
	if h.Path != "" && len(h.Cmd) > 0 {
		return fmt.Errorf("health: path and cmd are mutually exclusive")
	}
	h.applyDefaults()
	return nil
}

func (h *Health) applyDefaults() {
	if h.Interval == 0 {
		h.Interval = 10 * time.Second
	}
	if h.Timeout == 0 {
		h.Timeout = 5 * time.Second
	}
	if h.Retries == 0 {
		h.Retries = 5
	}
}

func (h *Health) String() string {
	if h == nil {
		return "none"
	}
	if h.Path != "" {
		return "GET " + h.Path
	}
	return strings.Join(h.Cmd, " ")
}
