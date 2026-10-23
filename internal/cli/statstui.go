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
)

// The TUI follows Bubble Tea's Elm architecture: Init fires effects, Update
// takes messages and returns new state, View draws from state. No I/O in View.
//
// Architectural decision: sampling runs on the SERVER, drawing on the CLIENT.
// That is why the model only holds finished numbers -- if hploy ever talks to
// a remote daemon, only the origin of the message changes.
type statsModel struct {
	engine   *engine.Client
	interval time.Duration

	hi      host.Info
	samples []engine.Sample
	err     error

	width  int
	height int
	ticks  int
	paused bool
}

type sampleMsg struct {
	hi      host.Info
	samples []engine.Sample
	err     error
}

type tickMsg time.Time

func runTUI(ctx context.Context, c *engine.Client, interval time.Duration) error {
	m := statsModel{engine: c, interval: interval}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	if err != nil && ctx.Err() != nil {
		return nil // Ctrl+C
	}
	return err
}

func (m statsModel) Init() tea.Cmd {
	return tea.Batch(m.collectCmd(), tick(m.interval))
}

func (m statsModel) collectCmd() tea.Cmd {
	eng := m.engine
	return func() tea.Msg {
		// Its own context: a stuck collection must not freeze the view.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		hi, samples, err := collect(ctx, eng)
		return sampleMsg{hi: hi, samples: samples, err: err}
	}
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m statsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "p", " ":
			m.paused = !m.paused
			return m, nil
		case "r":
			return m, m.collectCmd()
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case sampleMsg:
		m.hi, m.samples, m.err = msg.hi, msg.samples, msg.err
		m.ticks++

	case tickMsg:
		if m.paused {
			return m, tick(m.interval)
		}
		return m, tea.Batch(m.collectCmd(), tick(m.interval))
	}
	return m, nil
}

func (m statsModel) View() string {
	if m.ticks == 0 && m.err == nil {
		return "\n  " + ui.Dim.Render("collecting…") + "\n"
	}

	var s string
	s += hostPanel(m.hi) + "\n"

	if m.err != nil {
		s += ui.Bad.Render("collection failed: "+m.err.Error()) + "\n\n"
	}
	if len(m.samples) == 0 {
		s += ui.Note("no containers running") + "\n"
	} else {
		s += containerTable(m.samples, m.hi.MemTotal)
	}

	status := fmt.Sprintf("refreshing every %s", m.interval)
	if m.paused {
		status = ui.Warn.Render("paused")
	}
	help := ui.Dim.Render("q quit · p pause · r refresh now") +
		"  " + ui.Dim.Render("·") + "  " + ui.Dim.Render(status)

	return lipgloss.NewStyle().Padding(1, 2).Render(s + "\n" + help)
}
