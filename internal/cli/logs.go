package cli

import (
	"errors"
	"io"
	"os"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/spf13/cobra"
)

func cmdLogs() *cobra.Command {
	var opt engine.LogOptions
	cmd := &cobra.Command{
		Use:   "logs <container>",
		Short: "Mostra o log de um container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := motor(cmd.Context())
			if err != nil {
				return err
			}
			r, err := c.Logs(cmd.Context(), args[0], opt)
			if err != nil {
				return err
			}
			defer r.Close()

			// Saida crua, sem cor e sem tabela: log existe para passar por
			// grep, less e redirecionamento.
			_, err = io.Copy(os.Stdout, r)
			switch {
			case err == nil, errors.Is(err, io.EOF):
				return nil
			case cmd.Context().Err() != nil:
				return nil // Ctrl+C
			default:
				return err
			}
		},
	}
	cmd.Flags().BoolVarP(&opt.Follow, "follow", "f", false, "acompanha novas linhas")
	cmd.Flags().StringVarP(&opt.Tail, "tail", "n", "100", "quantas linhas finais mostrar (ou \"all\")")
	cmd.Flags().BoolVarP(&opt.Timestamps, "timestamps", "t", false, "prefixa cada linha com o horario")
	return cmd
}
