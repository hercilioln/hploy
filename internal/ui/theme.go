// Package ui holds color, formatting and tables.
//
// Everything about appearance lives here: commands assemble data, never
// colored strings. That way changing the theme means touching one file.
package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	Title  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	Header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244"))
	Dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	Ours   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	Good = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	Warn = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Bad  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// State colors a container state.
func State(state, health string) string {
	text := state
	if health != "" {
		text += " · " + health
	}
	switch {
	case health == "unhealthy":
		return Bad.Render(text)
	case health == "starting":
		return Warn.Render(text)
	case state == "running":
		return Good.Render(text)
	case state == "exited", state == "dead":
		return Bad.Render(text)
	default:
		return Dim.Render(text)
	}
}

// Level colors a percentage by severity. The thresholds are shared by CPU,
// memory and disk -- if they ever need to differ, they become a parameter.
func Level(p float64) lipgloss.Style {
	switch {
	case p >= 90:
		return Bad
	case p >= 70:
		return Warn
	default:
		return Good
	}
}

// Bar draws a fixed-width gauge. We use block characters rather than the
// Bubbles progress bar in non-interactive output so it stays readable when
// redirected to a file.
func Bar(p float64, width int) string {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	filled := int(p / 100 * float64(width))
	s := ""
	for i := 0; i < width; i++ {
		if i < filled {
			s += "█"
		} else {
			s += "░"
		}
	}
	return Level(p).Render(s)
}

// Bytes formats a byte count using base 1000 (kB, MB, GB).
//
// The docker CLI mixes the two bases -- memory in MiB (1024), network and disk
// in MB (1000) -- which makes the same magnitude look different in adjacent
// columns. We use a single base, so memory readings come out ~5% larger than
// in `docker stats`. Same quantity, different unit.
func Bytes(b uint64) string {
	const unit = 1000.0
	f := float64(b)
	if f < unit {
		return fmt.Sprintf("%dB", b)
	}
	for _, suffix := range []string{"kB", "MB", "GB", "TB", "PB"} {
		f /= unit
		if f < unit {
			if f < 10 {
				return fmt.Sprintf("%.1f%s", f, suffix)
			}
			return fmt.Sprintf("%.0f%s", f, suffix)
		}
	}
	return fmt.Sprintf("%.0fPB", f)
}

// Perc formats a percentage, or an em dash when the value does not exist yet.
func Perc(p float64, hasValue bool) string {
	if !hasValue {
		return Dim.Render("—")
	}
	return fmt.Sprintf("%.1f%%", p)
}

// Duration returns a compact form: 3d4h, 5h12m, 42s.
func Duration(d time.Duration) string {
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
