package spec

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Parse le o YAML e preenche o que nao vem do arquivo.
func Parse(b []byte) (*Spec, error) {
	var s Spec
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	// KnownFields faz chave desconhecida virar erro em vez de ser ignorada em
	// silencio. Typo em nome de campo e o bug mais chato de um arquivo de
	// config: tudo "funciona", so que a opcao nunca foi aplicada.
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}

	if s.Web != nil {
		s.Web.nome = "web"
	}
	for nome, sv := range s.Services {
		if sv == nil {
			return nil, fmt.Errorf("services.%s: bloco vazio", nome)
		}
		sv.nome = nome
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Bytes aceita "512m", "2g", "268435456".
type Bytes uint64

func (b *Bytes) UnmarshalYAML(n *yaml.Node) error {
	var bruto string
	if err := n.Decode(&bruto); err != nil {
		// Tambem aceita numero puro.
		var v uint64
		if err2 := n.Decode(&v); err2 != nil {
			return fmt.Errorf("memory: esperava texto como \"512m\" ou numero")
		}
		*b = Bytes(v)
		return nil
	}

	bruto = strings.TrimSpace(strings.ToLower(bruto))
	if bruto == "" {
		return nil
	}
	mult := uint64(1)
	switch {
	case strings.HasSuffix(bruto, "k"), strings.HasSuffix(bruto, "kb"):
		mult = 1 << 10
	case strings.HasSuffix(bruto, "m"), strings.HasSuffix(bruto, "mb"):
		mult = 1 << 20
	case strings.HasSuffix(bruto, "g"), strings.HasSuffix(bruto, "gb"):
		mult = 1 << 30
	}
	num := strings.TrimRight(bruto, "kmgb")
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return fmt.Errorf("memory %q: nao consegui interpretar", bruto)
	}
	if v < 0 {
		return fmt.Errorf("memory %q: nao pode ser negativo", bruto)
	}
	*b = Bytes(v * float64(mult))
	return nil
}

func (b Bytes) String() string {
	switch {
	case b == 0:
		return "sem limite"
	case b >= 1<<30:
		return fmt.Sprintf("%.3gG", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.3gM", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%dB", uint64(b))
	}
}

// Health aceita duas formas, porque servico web e servico de apoio precisam
// de sondas diferentes:
//
//	health: /up                              -> HTTP GET no caminho
//	health: { cmd: ["pg_isready", "-U", "x"] } -> comando dentro do container
type Health struct {
	Path     string        `yaml:"path"`
	Cmd      []string      `yaml:"cmd"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Retries  int           `yaml:"retries"`
}

func (h *Health) UnmarshalYAML(n *yaml.Node) error {
	// Atalho: health: /up
	if n.Kind == yaml.ScalarNode {
		var caminho string
		if err := n.Decode(&caminho); err != nil {
			return err
		}
		if !strings.HasPrefix(caminho, "/") {
			return fmt.Errorf("health %q: o atalho precisa ser um caminho comecando com /", caminho)
		}
		h.Path = caminho
		h.aplicarPadroes()
		return nil
	}

	// Forma completa. Tipo auxiliar para nao recursar no proprio Unmarshal.
	type cru Health
	var c cru
	if err := n.Decode(&c); err != nil {
		return err
	}
	*h = Health(c)
	if h.Path == "" && len(h.Cmd) == 0 {
		return fmt.Errorf("health: defina path ou cmd")
	}
	if h.Path != "" && len(h.Cmd) > 0 {
		return fmt.Errorf("health: path e cmd sao exclusivos")
	}
	h.aplicarPadroes()
	return nil
}

func (h *Health) aplicarPadroes() {
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
		return "nenhuma"
	}
	if h.Path != "" {
		return "GET " + h.Path
	}
	return strings.Join(h.Cmd, " ")
}
