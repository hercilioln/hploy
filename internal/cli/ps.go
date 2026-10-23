package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdPs() *cobra.Command {
	var todos bool
	cmd := &cobra.Command{
		Use:     "ps",
		Short:   "Lista os containers da maquina",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := motor(cmd.Context())
			if err != nil {
				return err
			}
			lista, err := c.List(cmd.Context(), todos)
			if err != nil {
				return err
			}
			if len(lista) == 0 {
				fmt.Println(ui.Aviso("nenhum container encontrado"))
				return nil
			}

			t := ui.NewTable("nome", "app", "estado", "imagem", "portas", "criado")
			for _, ct := range lista {
				app := ui.Apagado.Render("—")
				if ct.Managed() {
					app = ui.Nosso.Render(ct.App)
				}
				t.Row(
					ct.Name,
					app,
					ui.Estado(ct.State, ct.Health),
					encurta(ct.Image, 34),
					portas(ct),
					ui.Duracao(idade(ct)),
				)
			}
			fmt.Print(t.String())

			geridos := 0
			for _, ct := range lista {
				if ct.Managed() {
					geridos++
				}
			}
			fmt.Println()
			fmt.Println(ui.Aviso("%d containers, %d geridos pelo hploy", len(lista), geridos))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&todos, "all", "a", false, "inclui containers parados")
	return cmd
}

func portas(ct engine.Container) string {
	if len(ct.Ports) == 0 {
		return ui.Apagado.Render("—")
	}
	s := strings.Join(ct.Ports, " ")
	// Porta publicada em todas as interfaces merece destaque: e o erro que
	// expoe banco de dados para a internet.
	if strings.Contains(s, "*:") {
		return ui.Atencao.Render(encurta(s, 28))
	}
	return ui.Apagado.Render(encurta(s, 28))
}

func encurta(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func idade(ct engine.Container) time.Duration {
	return time.Since(ct.CreatedAt)
}
