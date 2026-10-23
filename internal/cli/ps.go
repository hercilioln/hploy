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
	var all bool
	cmd := &cobra.Command{
		Use:     "ps",
		Short:   "List the containers on the machine",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := dockerEngine(cmd.Context())
			if err != nil {
				return err
			}
			list, err := c.List(cmd.Context(), all)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println(ui.Note("no containers found"))
				return nil
			}

			t := ui.NewTable("name", "app", "state", "image", "ports", "created")
			for _, ct := range list {
				app := ui.Dim.Render("—")
				if ct.Managed() {
					app = ui.Ours.Render(ct.App)
				}
				t.Row(
					ct.Name,
					app,
					ui.State(ct.State, ct.Health),
					truncate(ct.Image, 34),
					portList(ct),
					ui.Duration(time.Since(ct.CreatedAt)),
				)
			}
			fmt.Print(t.String())

			managed := 0
			for _, ct := range list {
				if ct.Managed() {
					managed++
				}
			}
			fmt.Println()
			fmt.Println(ui.Note("%d containers, %d managed by hploy", len(list), managed))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include stopped containers")
	return cmd
}

func portList(ct engine.Container) string {
	if len(ct.Ports) == 0 {
		return ui.Dim.Render("—")
	}
	s := strings.Join(ct.Ports, " ")
	// A port bound to every interface is worth flagging.
	if strings.Contains(s, "*:") {
		return ui.Warn.Render(truncate(s, 28))
	}
	return ui.Dim.Render(truncate(s, 28))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
