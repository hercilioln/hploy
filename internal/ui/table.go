package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Table monta tabela de largura automatica.
//
// Nao usamos a tabela do Lip Gloss porque precisamos medir a largura ignorando
// os codigos de cor -- uma celula colorida tem mais bytes que caracteres
// visiveis, e medir errado desalinha a coluna toda.
type Table struct {
	cabecalhos []string
	linhas     [][]string
	direita    map[int]bool
}

func NewTable(cabecalhos ...string) *Table {
	return &Table{cabecalhos: cabecalhos, direita: map[int]bool{}}
}

// AlignRight marca colunas numericas, que leem melhor alinhadas a direita.
func (t *Table) AlignRight(cols ...int) *Table {
	for _, c := range cols {
		t.direita[c] = true
	}
	return t
}

func (t *Table) Row(celulas ...string) *Table {
	t.linhas = append(t.linhas, celulas)
	return t
}

func (t *Table) Len() int { return len(t.linhas) }

func (t *Table) String() string {
	if len(t.cabecalhos) == 0 {
		return ""
	}
	larguras := make([]int, len(t.cabecalhos))
	for i, h := range t.cabecalhos {
		larguras[i] = lipgloss.Width(h)
	}
	for _, l := range t.linhas {
		for i, c := range l {
			if i < len(larguras) {
				if w := lipgloss.Width(c); w > larguras[i] {
					larguras[i] = w
				}
			}
		}
	}

	var b strings.Builder
	escreve := func(celulas []string, estilo func(string) string) {
		var linha strings.Builder
		for i, c := range celulas {
			if i >= len(larguras) {
				continue
			}
			pad := larguras[i] - lipgloss.Width(c)
			if pad < 0 {
				pad = 0
			}
			espaco := strings.Repeat(" ", pad)
			if t.direita[i] {
				linha.WriteString(espaco + estilo(c))
			} else {
				linha.WriteString(estilo(c) + espaco)
			}
			if i < len(celulas)-1 {
				linha.WriteString("   ")
			}
		}
		// Espaco no fim da linha suja o terminal e aparece em diff e em pipe.
		b.WriteString(strings.TrimRight(linha.String(), " "))
		b.WriteString("\n")
	}

	escreve(t.cabecalhos, func(s string) string { return Cabecalho.Render(strings.ToUpper(s)) })
	for _, l := range t.linhas {
		escreve(l, func(s string) string { return s })
	}
	return b.String()
}

// Aviso formata mensagem de nada-encontrado.
func Aviso(formato string, args ...any) string {
	return Apagado.Render(fmt.Sprintf(formato, args...))
}
