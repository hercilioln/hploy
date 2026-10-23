package cli

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/host"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

// A TUI segue a arquitetura Elm do Bubble Tea: Init dispara efeitos, Update
// recebe mensagens e devolve novo estado, View desenha a partir do estado.
// Nada de I/O no View.
//
// Decisao de arquitetura: a coleta roda no SERVIDOR e o desenho no CLIENTE.
// Por isso o modelo so guarda numeros ja prontos -- se um dia o hploy falar
// com um daemon remoto, so a origem da mensagem muda.
type modeloStats struct {
	motor     *engine.Client
	intervalo time.Duration

	hi       host.Info
	amostras []engine.Sample
	erro     error

	largura int
	altura  int
	tiques  int
	pausado bool
}

type msgAmostra struct {
	hi       host.Info
	amostras []engine.Sample
	erro     error
}

type msgTique time.Time

func rodarTUI(ctx context.Context, c *engine.Client, intervalo time.Duration) error {
	m := modeloStats{motor: c, intervalo: intervalo}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	if err != nil && ctx.Err() != nil {
		return nil // Ctrl+C
	}
	return err
}

func (m modeloStats) Init() tea.Cmd {
	return tea.Batch(m.coletarCmd(), tique(m.intervalo))
}

func (m modeloStats) coletarCmd() tea.Cmd {
	motor := m.motor
	return func() tea.Msg {
		// Context proprio: uma coleta travada nao pode congelar a tela.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		hi, amostras, err := coletar(ctx, motor)
		return msgAmostra{hi: hi, amostras: amostras, erro: err}
	}
}

func tique(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return msgTique(t) })
}

func (m modeloStats) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "p", " ":
			m.pausado = !m.pausado
			return m, nil
		case "r":
			return m, m.coletarCmd()
		}

	case tea.WindowSizeMsg:
		m.largura, m.altura = msg.Width, msg.Height

	case msgAmostra:
		m.hi, m.amostras, m.erro = msg.hi, msg.amostras, msg.erro
		m.tiques++

	case msgTique:
		if m.pausado {
			return m, tique(m.intervalo)
		}
		return m, tea.Batch(m.coletarCmd(), tique(m.intervalo))
	}
	return m, nil
}

func (m modeloStats) View() string {
	if m.tiques == 0 && m.erro == nil {
		return "\n  " + ui.Apagado.Render("coletando…") + "\n"
	}

	var s string
	s += painelHost(m.hi) + "\n"

	if m.erro != nil {
		s += ui.Ruim.Render("erro na coleta: "+m.erro.Error()) + "\n\n"
	}
	if len(m.amostras) == 0 {
		s += ui.Aviso("nenhum container em execucao") + "\n"
	} else {
		s += tabelaContainers(m.amostras, m.hi.MemTotal)
	}

	estado := fmt.Sprintf("atualiza a cada %s", m.intervalo)
	if m.pausado {
		estado = ui.Atencao.Render("pausado")
	}
	ajuda := ui.Apagado.Render("q sair · p pausar · r atualizar agora") +
		"  " + ui.Apagado.Render("·") + "  " + ui.Apagado.Render(estado)

	return lipgloss.NewStyle().Padding(1, 2).Render(s + "\n" + ajuda)
}

// garante que o pacote cobra continue importado neste arquivo quando a TUI
// for extraida para outro lugar.
var _ = cobra.Command{}
