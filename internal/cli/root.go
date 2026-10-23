// Package cli wires up the command surface.
//
// House rule: a command assembles data and calls the ui package to render it.
// No colored strings are written in here.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/spf13/cobra"
)

// Version is overridden at build time:
// -ldflags "-X .../internal/cli.Version=v0.2.0"
var Version = "dev"

var socketFlag string

func Execute() {
	// Ctrl+C has to close the stats stream and the log follow gracefully,
	// not kill the process mid-frame.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := root().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hploy",
		Short: "Orchestrate containers and deploy apps on a VPS",
		Long: "hploy — container orchestration from the terminal.\n\n" +
			"This version is READ ONLY: it observes what already runs on the\n" +
			"machine and never creates, changes or removes anything.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&socketFlag, "socket", engine.DefaultSocket,
		"path to the Docker socket")

	cmd.AddCommand(cmdPs(), cmdStats(), cmdLogs(), cmdVersion())
	return cmd
}

// dockerEngine returns a verified client. Failing here, with a clear message,
// beats failing halfway through a command.
func dockerEngine(ctx context.Context) (*engine.Client, error) {
	c := engine.New(socketFlag)
	if _, err := c.Ping(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the hploy and Docker daemon versions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Println("hploy", Version)
			c := engine.New(socketFlag)
			v, err := c.Ping(cmd.Context())
			if err != nil {
				fmt.Println("docker: unavailable —", err)
				return nil
			}
			fmt.Println("docker", v)
			return nil
		},
	}
}
