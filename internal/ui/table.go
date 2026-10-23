package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Table renders an auto-width table.
//
// We do not use the Lip Gloss table because we need to measure width ignoring
// the color escape codes -- a styled cell has more bytes than visible
// characters, and measuring wrong misaligns the entire column.
type Table struct {
	headers []string
	rows    [][]string
	right   map[int]bool
}

func NewTable(headers ...string) *Table {
	return &Table{headers: headers, right: map[int]bool{}}
}

// AlignRight marks numeric columns, which read better right-aligned.
func (t *Table) AlignRight(cols ...int) *Table {
	for _, c := range cols {
		t.right[c] = true
	}
	return t
}

func (t *Table) Row(cells ...string) *Table {
	t.rows = append(t.rows, cells)
	return t
}

func (t *Table) Len() int { return len(t.rows) }

func (t *Table) String() string {
	if len(t.headers) == 0 {
		return ""
	}
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) {
				if w := lipgloss.Width(c); w > widths[i] {
					widths[i] = w
				}
			}
		}
	}

	var b strings.Builder
	write := func(cells []string, style func(string) string) {
		var line strings.Builder
		for i, c := range cells {
			if i >= len(widths) {
				continue
			}
			pad := widths[i] - lipgloss.Width(c)
			if pad < 0 {
				pad = 0
			}
			spaces := strings.Repeat(" ", pad)
			if t.right[i] {
				line.WriteString(spaces + style(c))
			} else {
				line.WriteString(style(c) + spaces)
			}
			if i < len(cells)-1 {
				line.WriteString("   ")
			}
		}
		// Trailing spaces dirty the terminal and show up in diffs and pipes.
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
	}

	write(t.headers, func(s string) string { return Header.Render(strings.ToUpper(s)) })
	for _, r := range t.rows {
		write(r, func(s string) string { return s })
	}
	return b.String()
}

// Note formats a nothing-found message.
func Note(format string, args ...any) string {
	return Dim.Render(fmt.Sprintf(format, args...))
}
