package spec

import "testing"

func TestExemploEValido(t *testing.T) {
	s, err := Load("../../exemplo/hploy.yaml")
	if err != nil {
		t.Fatalf("o exemplo do repositorio nao deveria falhar: %v", err)
	}
	if s.App != "exemplo" {
		t.Errorf("app = %q, queria \"exemplo\"", s.App)
	}
	if s.Web == nil || s.Web.Port != 80 {
		t.Error("web deveria existir com porta 80")
	}
	// O atalho "health: /" tem de virar um Health com Path e padroes.
	if s.Web.Health == nil || s.Web.Health.Path != "/" {
		t.Errorf("atalho de health nao virou Path: %+v", s.Web.Health)
	}
	if s.Web.Health.Retries != 5 {
		t.Errorf("retries padrao = %d, queria 5", s.Web.Health.Retries)
	}
	if got := s.Services["db"].Health.Cmd[0]; got != "pg_isready" {
		t.Errorf("health.cmd do db = %q", got)
	}
	if s.Services["db"].Resources.Memory != 128<<20 {
		t.Errorf("memory do db = %d", s.Services["db"].Resources.Memory)
	}
	// A ordem de subida tem de deixar o web por ultimo.
	ordem := s.EmOrdemDeSubida()
	if ordem[len(ordem)-1] != s.Web {
		t.Error("o web deveria subir por ultimo, depois do banco")
	}
}

func TestValidacaoPegaOsErros(t *testing.T) {
	casos := []struct {
		nome   string
		yaml   string
		espera string
	}{
		{
			nome: "servico de apoio com porta publicada",
			yaml: `
version: 1
app: teste
services:
  db:
    image: postgres:17-alpine
    port: 5432
    resources: { memory: 64m }
`,
			espera: "port nao e permitida em servico de apoio",
		},
		{
			nome: "volume usado sem declarar",
			yaml: `
version: 1
app: teste
services:
  db:
    image: postgres:17-alpine
    volumes: ["fantasma:/dados"]
    resources: { memory: 64m }
`,
			espera: "nao declarado no bloco volumes",
		},
		{
			nome: "sem limite de memoria",
			yaml: `
version: 1
app: teste
services:
  db:
    image: postgres:17-alpine
`,
			espera: "resources.memory nao definido",
		},
		{
			nome: "web sem dominio",
			yaml: `
version: 1
app: teste
web:
  image: nginx:alpine
  port: 80
  resources: { memory: 64m }
`,
			espera: "domains e obrigatorio",
		},
		{
			nome: "campo desconhecido (typo)",
			yaml: `
version: 1
app: teste
web:
  image: nginx:alpine
  port: 80
  resorces: { memory: 64m }
`,
			espera: "field resorces not found",
		},
		{
			nome:   "nome de app invalido",
			yaml:   "version: 1\napp: Exemplo_Com_Maiuscula\n",
			espera: "invalido",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml))
			if err == nil {
				t.Fatal("esperava erro, nao veio nenhum")
			}
			if !contem(err.Error(), c.espera) {
				t.Errorf("erro foi %q, esperava conter %q", err, c.espera)
			}
		})
	}
}

func TestHashEstavelEsensivel(t *testing.T) {
	s, err := Load("../../exemplo/hploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	a := s.Web.Hash(s.App, nil)
	b := s.Web.Hash(s.App, nil)
	if a != b {
		t.Error("o hash tem de ser estavel entre chamadas")
	}

	// Valor de segredo NAO entra no hash: trocar senha nao recria container.
	c := s.Web.Hash(s.App, map[string]string{"SENHA": "outra"})
	if a != c {
		t.Error("valor de env nao deveria mudar o hash")
	}

	// Mas mudar a imagem tem de mudar.
	s.Web.Image = "nginx:1.28-alpine"
	if s.Web.Hash(s.App, nil) == a {
		t.Error("mudar a imagem tinha de mudar o hash")
	}
}

func contem(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
