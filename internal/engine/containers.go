package engine

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// LabelApp e a etiqueta que marca um container como nosso. Usar label como
// fonte de verdade do runtime significa que, se o daemon do hploy morrer, o
// estado nao se perde -- e o `docker ps` continua legivel para humano.
const LabelApp = "hploy.app"

// LabelRelease guarda o commit que gerou a imagem.
const LabelRelease = "hploy.release"

// resposta de GET /containers/json -- so os campos que usamos.
type apiContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

// O Docker nao devolve o estado de saude em campo proprio na listagem: ele
// vem embutido no texto do Status, como "Up 2 hours (healthy)". E o que o
// proprio CLI do Docker faz -- e evita um inspect por container.
var reHealth = regexp.MustCompile(`\((healthy|unhealthy|health: starting|starting)\)`)

// List devolve os containers, ordenados com os nossos primeiro.
func (c *Client) List(ctx context.Context, all bool) ([]Container, error) {
	caminho := "/containers/json"
	if all {
		caminho += q("all", "1")
	}

	var brutos []apiContainer
	if err := c.getJSON(ctx, caminho, &brutos); err != nil {
		return nil, err
	}

	out := make([]Container, 0, len(brutos))
	for _, b := range brutos {
		out = append(out, Container{
			ID:        curto(b.ID),
			Name:      nome(b.Names),
			Image:     b.Image,
			State:     b.State,
			Status:    b.Status,
			Health:    saude(b.Status),
			App:       b.Labels[LabelApp],
			CreatedAt: time.Unix(b.Created, 0),
			Ports:     portas(b),
		})
	}

	// Nossos containers primeiro, depois alfabetico. Quem roda o comando
	// quer ver o que e dele no topo.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Managed() != out[j].Managed() {
			return out[i].Managed()
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func curto(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// Os nomes vem com barra na frente ("/nginx-proxy") e um container pode ter
// varios alias; o primeiro e o principal.
func nome(nomes []string) string {
	if len(nomes) == 0 {
		return "<sem nome>"
	}
	return strings.TrimPrefix(nomes[0], "/")
}

func saude(status string) string {
	m := reHealth.FindStringSubmatch(status)
	if m == nil {
		return ""
	}
	if strings.Contains(m[1], "starting") {
		return "starting"
	}
	return m[1]
}

func portas(b apiContainer) []string {
	vistas := map[string]bool{}
	var out []string
	for _, p := range b.Ports {
		var s string
		if p.PublicPort != 0 {
			ip := p.IP
			if ip == "0.0.0.0" || ip == "::" {
				ip = "*" // deixa obvio que esta aberto para fora
			}
			s = fmt.Sprintf("%s:%d->%d/%s", ip, p.PublicPort, p.PrivatePort, p.Type)
		} else {
			s = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		}
		if !vistas[s] {
			vistas[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
