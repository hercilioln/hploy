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
		Short: "Show a container's log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dockerEngine(cmd.Context())
			if err != nil {
				return err
			}
			r, err := c.Logs(cmd.Context(), args[0], opt)
			if err != nil {
				return err
			}
			defer r.Close()

			// Raw output, no color and no table: logs exist to be piped into
			// grep, less and redirection.
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
	cmd.Flags().BoolVarP(&opt.Follow, "follow", "f", false, "keep streaming new lines")
	cmd.Flags().StringVarP(&opt.Tail, "tail", "n", "100", "how many trailing lines to show (or \"all\")")
	cmd.Flags().BoolVarP(&opt.Timestamps, "timestamps", "t", false, "prefix each line with its time")
	return cmd
}
