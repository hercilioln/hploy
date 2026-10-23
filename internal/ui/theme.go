// Package ui concentra cor, formatacao e tabela.
//
// Tudo que e aparencia mora aqui: os comandos montam dados, nunca strings
// coloridas. Assim trocar o tema e mexer num arquivo.
package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	Titulo    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	Cabecalho = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244"))
	Apagado   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	Nosso     = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	OK      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	Atencao = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Ruim    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// Estado colore o estado do container.
func Estado(estado, saude string) string {
	texto := estado
	if saude != "" {
		texto += " · " + saude
	}
	switch {
	case saude == "unhealthy":
		return Ruim.Render(texto)
	case saude == "starting":
		return Atencao.Render(texto)
	case estado == "running":
		return OK.Render(texto)
	case estado == "exited", estado == "dead":
		return Ruim.Render(texto)
	default:
		return Apagado.Render(texto)
	}
}

// Nivel colore um percentual segundo a gravidade. Os limites sao os mesmos
// para CPU, memoria e disco -- se um dia divergirem, viram parametro.
func Nivel(p float64) lipgloss.Style {
	switch {
	case p >= 90:
		return Ruim
	case p >= 70:
		return Atencao
	default:
		return OK
	}
}

// Barra desenha um medidor de largura fixa. Usamos blocos cheios em vez da
// barra do Bubbles no modo nao-interativo para a saida continuar legivel
// quando redirecionada para arquivo.
func Barra(p float64, largura int) string {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	cheios := int(p / 100 * float64(largura))
	s := ""
	for i := 0; i < largura; i++ {
		if i < cheios {
			s += "█"
		} else {
			s += "░"
		}
	}
	return Nivel(p).Render(s)
}

// Bytes formata em unidade legivel usando base 1000 (kB, MB, GB).
//
// O CLI do docker mistura as duas bases -- memoria em MiB (1024) e rede/disco
// em MB (1000) -- o que faz o mesmo numero aparecer diferente em colunas
// vizinhas. Aqui usamos uma base so, entao os valores de memoria aparecem
// ~5% maiores que no `docker stats`. E a mesma grandeza, outra unidade.
func Bytes(b uint64) string {
	const u = 1000.0
	f := float64(b)
	if f < u {
		return fmt.Sprintf("%dB", b)
	}
	unidades := []string{"kB", "MB", "GB", "TB", "PB"}
	for _, un := range unidades {
		f /= u
		if f < u {
			if f < 10 {
				return fmt.Sprintf("%.1f%s", f, un)
			}
			return fmt.Sprintf("%.0f%s", f, un)
		}
	}
	return fmt.Sprintf("%.0fPB", f)
}

// Perc formata percentual, ou travessao quando o valor ainda nao existe.
func Perc(p float64, temValor bool) string {
	if !temValor {
		return Apagado.Render("—")
	}
	return fmt.Sprintf("%.1f%%", p)
}

// Duracao devolve forma compacta: 3d4h, 5h12m, 42s.
func Duracao(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
