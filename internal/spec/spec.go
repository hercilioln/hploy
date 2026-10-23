// Package spec le e valida o hploy.yaml.
//
// O spec descreve o estado DESEJADO de um app. Ele nao sabe nada de Docker --
// quem traduz para container, rede e volume e o pacote plan. Essa separacao e
// o que permite validar um spec sem daemon nenhum por perto.
package spec

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Spec e um hploy.yaml inteiro.
type Spec struct {
	Version  int                 `yaml:"version"`
	App      string              `yaml:"app"`
	Domains  []string            `yaml:"domains"`
	Web      *Service            `yaml:"web"`
	Services map[string]*Service `yaml:"services"`
	Volumes  map[string]Volume   `yaml:"volumes"`

	// Origem guarda de qual arquivo o spec veio, para a mensagem de erro
	// apontar o lugar certo.
	Origem string `yaml:"-"`
}

// Service e um container.
type Service struct {
	Image     string    `yaml:"image"`
	Command   []string  `yaml:"command"`
	Port      int       `yaml:"port"`
	Health    *Health   `yaml:"health"`
	Env       Env       `yaml:"env"`
	Volumes   []string  `yaml:"volumes"` // "nome:/caminho/no/container"
	Resources Resources `yaml:"resources"`

	// nome e preenchido na leitura; nao vem do arquivo.
	nome string `yaml:"-"`
}

func (s *Service) Name() string { return s.nome }

// Volume por enquanto nao tem opcao; existe para o spec declarar quais
// volumes o app usa, e o plan criar os que faltam.
type Volume struct {
	// reservado para driver/opts no futuro
}

// Env declara QUAIS variaveis o servico precisa -- nunca os valores.
//
// Valor de segredo em arquivo versionado e questao de tempo para vazar. Os
// valores vem de ~/.hploy/apps/<app>/env, que nao vai para o git.
type Env struct {
	Required []string          `yaml:"required"`
	Optional []string          `yaml:"optional"`
	Literal  map[string]string `yaml:"literal"` // so para valor nao-secreto
}

// Resources limita o container.
//
// Numa VPS de 4 GB com dois apps, limite de memoria e a diferenca entre "um
// app morre" e "a maquina entra em swap e tudo cai junto". Por isso o plan
// avisa quando falta.
type Resources struct {
	Memory Bytes  `yaml:"memory"`
	CPUs   string `yaml:"cpus"`
}

var reNome = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

// Load le e valida um hploy.yaml.
func Load(caminho string) (*Spec, error) {
	b, err := os.ReadFile(caminho)
	if err != nil {
		return nil, err
	}
	s, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", caminho, err)
	}
	s.Origem = caminho
	return s, nil
}

// Validate junta TODOS os problemas antes de devolver, em vez de parar no
// primeiro. Quem esta escrevendo o arquivo prefere ver a lista inteira.
func (s *Spec) Validate() error {
	var problemas []string
	erro := func(formato string, args ...any) {
		problemas = append(problemas, fmt.Sprintf(formato, args...))
	}

	if s.Version != 1 {
		erro("version deve ser 1 (veio %d)", s.Version)
	}
	if s.App == "" {
		erro("app e obrigatorio")
	} else if !reNome.MatchString(s.App) {
		erro("app %q invalido: use minusculas, numeros e hifen, comecando e terminando com alfanumerico", s.App)
	}
	if s.Web == nil && len(s.Services) == 0 {
		erro("o spec nao declara nenhum servico: falta web ou services")
	}

	// Valida cada servico, incluindo o web.
	for _, sv := range s.todos() {
		prefixo := "servico " + sv.Name()
		if !reNome.MatchString(sv.Name()) {
			erro("%s: nome invalido", prefixo)
		}
		if sv.Image == "" {
			erro("%s: image e obrigatoria", prefixo)
		}
		if sv.Resources.Memory == 0 {
			erro("%s: resources.memory nao definido — sem limite, um app sozinho derruba a maquina", prefixo)
		}
		for _, v := range sv.Volumes {
			nome, caminho, ok := strings.Cut(v, ":")
			switch {
			case !ok:
				erro("%s: volume %q deve ser \"nome:/caminho\"", prefixo, v)
			case !strings.HasPrefix(caminho, "/"):
				erro("%s: volume %q: o caminho tem de ser absoluto", prefixo, v)
			case !reNome.MatchString(nome):
				erro("%s: volume %q: nome invalido", prefixo, nome)
			default:
				if _, declarado := s.Volumes[nome]; !declarado {
					erro("%s: volume %q usado mas nao declarado no bloco volumes", prefixo, nome)
				}
			}
		}
	}

	if s.Web != nil {
		if s.Web.Port == 0 {
			erro("web: port e obrigatoria — e por ela que o proxy encontra o app")
		}
		if len(s.Domains) == 0 {
			erro("domains e obrigatorio quando ha um servico web")
		}
	} else if len(s.Domains) > 0 {
		erro("domains declarado mas nao ha servico web para atender")
	}

	// Serviço de apoio com porta e o erro que expoe banco para a internet.
	for nome, sv := range s.Services {
		if sv.Port != 0 {
			erro("services.%s: port nao e permitida em servico de apoio — use a rede interna", nome)
		}
	}

	if len(problemas) > 0 {
		sort.Strings(problemas)
		return fmt.Errorf("spec invalido:\n  - %s", strings.Join(problemas, "\n  - "))
	}
	return nil
}

// todos devolve web + services, em ordem estavel.
func (s *Spec) todos() []*Service {
	var out []*Service
	if s.Web != nil {
		out = append(out, s.Web)
	}
	nomes := make([]string, 0, len(s.Services))
	for n := range s.Services {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	for _, n := range nomes {
		out = append(out, s.Services[n])
	}
	return out
}

// Services ordenados, com o web por ultimo.
//
// A ordem importa no apply: o banco tem de estar de pe antes do web, senao a
// aplicacao sobe sem conseguir conectar.
func (s *Spec) EmOrdemDeSubida() []*Service {
	todos := s.todos()
	out := make([]*Service, 0, len(todos))
	for _, sv := range todos {
		if sv != s.Web {
			out = append(out, sv)
		}
	}
	if s.Web != nil {
		out = append(out, s.Web)
	}
	return out
}

// EnvNames devolve todas as variaveis que o servico espera.
func (sv *Service) EnvNames() []string {
	out := append([]string{}, sv.Env.Required...)
	out = append(out, sv.Env.Optional...)
	for k := range sv.Env.Literal {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
