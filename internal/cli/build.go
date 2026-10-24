package cli

import (
	"fmt"
	"os"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/plan"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdBuild() *cobra.Command {
	var file, only string
	var noCache, pull, verbose bool

	cmd := &cobra.Command{
		Use:   "build [app]",
		Short: "Build the images declared with a build block",
		Long: "Builds and leaves the result on the machine. Nothing is restarted: the\n" +
			"new image makes the running container stale, which plan then reports\n" +
			"and apply carries out.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveTarget(args, file)
			if err != nil {
				return err
			}
			eng, err := dockerEngine(cmd.Context())
			if err != nil {
				return err
			}

			var todo []*spec.Service
			for _, sv := range t.Spec.StartOrder() {
				if sv.Build == nil || (only != "" && sv.Name() != only) {
					continue
				}
				todo = append(todo, sv)
			}
			if len(todo) == 0 {
				if only != "" {
					return fmt.Errorf("service %q has no build block", only)
				}
				fmt.Println(ui.Note("no service in this spec is built from a Dockerfile"))
				return nil
			}

			fmt.Println(ui.Title.Render("app " + t.Spec.App))
			fmt.Println()

			out, logs := output(ui.NewConsole(os.Stdout), verbose)
			for _, sv := range todo {
				err := plan.BuildService(cmd.Context(), eng, t, sv,
					engine.BuildOptions{NoCache: noCache, Pull: pull}, out)
				if err != nil {
					logs.dump()
					return fmt.Errorf("%s: %w", sv.Name(), err)
				}
			}

			fmt.Println()
			fmt.Println(ui.Good.Render("built"), ui.Note("· run `hploy plan` to see what it changes"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	cmd.Flags().StringVar(&only, "service", "", "build only this service")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "ignore the layer cache")
	cmd.Flags().BoolVar(&pull, "pull", false, "refresh the base image")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show the full build output")
	return cmd
}
