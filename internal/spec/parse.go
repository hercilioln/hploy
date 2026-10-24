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
	// Unknown keys are errors, so a typo does not silently do nothing.
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
	// A built service needs a tag to be referred to by; the rest of the
	// program then treats built and pulled images the same way.
	for _, sv := range s.all() {
		if sv.Image == "" && sv.Build != nil {
			sv.Image = fmt.Sprintf("%s-%s:latest", s.App, sv.Name())
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// MinMemory is the floor Docker enforces: below it, container create fails
// with "Minimum memory limit allowed is 6MB" -- after the image was built and
// the network and volumes were created, which is the worst possible moment to
// find out.
const MinMemory Bytes = 6 << 20

// Bytes accepts "512m", "2g", "268435456".
//
// A bare number is bytes, as it is for Docker itself. That makes "256" mean
// 256 bytes, which is never what someone means, so Validate rejects anything
// under MinMemory and says so.
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

// Health accepts three shapes:
//
//	health: tcp                                -> the port accepts a connection
//	health: /up                                -> HTTP GET on the port
//	health: { cmd: ["pg_isready", "-U", "x"] } -> command in the container
//
// tcp is the one that cannot be wrong about the protocol: a port that accepts
// a connection is up whether it then speaks HTTP, FastCGI or Postgres. A path
// says more when the port really is HTTP, and a command is the only option
// for a container with nothing listening at all.
type Health struct {
	TCP      bool          `yaml:"tcp"`
	Path     string        `yaml:"path"`
	Cmd      []string      `yaml:"cmd"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Retries  int           `yaml:"retries"`
}

func (h *Health) UnmarshalYAML(n *yaml.Node) error {
	// Shorthands: health: tcp and health: /up
	if n.Kind == yaml.ScalarNode {
		var text string
		if err := n.Decode(&text); err != nil {
			return err
		}
		switch {
		case text == "tcp":
			h.TCP = true
		case strings.HasPrefix(text, "/"):
			h.Path = text
		default:
			return fmt.Errorf("health %q: the shorthand is either tcp or a path starting with /", text)
		}
		h.applyDefaults()
		return nil
	}

	// A helper type, so decoding does not recurse into this method.
	type raw Health
	var r raw
	if err := n.Decode(&r); err != nil {
		return err
	}
	*h = Health(r)

	set := 0
	for _, on := range []bool{h.TCP, h.Path != "", len(h.Cmd) > 0} {
		if on {
			set++
		}
	}
	if set == 0 {
		return fmt.Errorf("health: set one of tcp, path or cmd")
	}
	if set > 1 {
		return fmt.Errorf("health: tcp, path and cmd are mutually exclusive")
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
	switch {
	case h == nil:
		return "none"
	case h.TCP:
		return "a connection on the port"
	case h.Path != "":
		return "GET " + h.Path
	default:
		return strings.Join(h.Cmd, " ")
	}
}
