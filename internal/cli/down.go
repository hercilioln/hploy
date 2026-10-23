package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/plan"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdDown() *cobra.Command {
	var file string
	var withVolumes, assumeYes bool

	cmd := &cobra.Command{
		Use:   "down [app]",
		Short: "Stop and remove an app's containers",
		Long: "Removes the containers and the network. Volumes are kept unless you ask\n" +
			"for them: they hold the data, and a spec can be reapplied but a\n" +
			"database cannot be un-deleted.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appName(args, file)
			if err != nil {
				return err
			}
			eng, err := dockerEngine(cmd.Context())
			if err != nil {
				return err
			}

			containers, network, volumes, err := ownedBy(cmd.Context(), eng, app)
			if err != nil {
				return err
			}
			if len(containers) == 0 && network == "" && len(volumes) == 0 {
				fmt.Println(ui.Note("nothing of app %q is on this machine", app))
				return nil
			}

			fmt.Println(ui.Title.Render("app " + app))
			fmt.Println()

			// Pad the plain names, then style: a styled string carries escape
			// codes that would throw the column off.
			nameW := 0
			for _, c := range containers {
				nameW = max(nameW, len(c.Name))
			}
			for _, v := range volumes {
				nameW = max(nameW, len(v))
			}
			nameW = max(nameW, len(network))

			line := func(mark, kind, name, note string) {
				text := fmt.Sprintf("  %s %s  %s  %s", mark,
					ui.Dim.Render(kind)+pad(len("container")-len(kind)),
					name+pad(nameW-len(name)), note)
				fmt.Println(strings.TrimRight(text, " "))
			}
			for _, c := range containers {
				line(ui.Bad.Render("-"), "container", c.Name, ui.Dim.Render(c.State))
			}
			if network != "" {
				line(ui.Bad.Render("-"), "network", network, "")
			}
			for _, v := range volumes {
				if withVolumes {
					line(ui.Bad.Render("-"), "volume", v, ui.Warn.Render("the data goes with it"))
				} else {
					line(ui.Dim.Render("="), "volume", v, ui.Dim.Render("kept"))
				}
			}
			fmt.Println()

			if withVolumes && len(volumes) > 0 {
				fmt.Println(ui.Bad.Render("this deletes data and cannot be undone"))
			}
			if !assumeYes {
				ok, err := confirmWith("remove these? [y/N] ")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println(ui.Note("cancelled"))
					return nil
				}
			}
			fmt.Println()

			step := stepPrinter()
			for _, c := range containers {
				step(plan.Remove, plan.KindContainer, c.Name, "")
				if c.State == "running" {
					if err := eng.Stop(cmd.Context(), c.Name, 10*time.Second); err != nil {
						return err
					}
				}
				if err := eng.Remove(cmd.Context(), c.Name, false); err != nil {
					return err
				}
			}
			if network != "" {
				step(plan.Remove, plan.KindNetwork, network, "")
				if err := eng.RemoveNetwork(cmd.Context(), network); err != nil {
					return err
				}
			}
			if withVolumes {
				for _, v := range volumes {
					step(plan.Remove, plan.KindVolume, v, "")
					if err := eng.RemoveVolume(cmd.Context(), v); err != nil {
						return err
					}
				}
			}

			fmt.Println()
			fmt.Println(ui.Good.Render("down"))
			if len(volumes) > 0 && !withVolumes {
				fmt.Println(ui.Note("volumes kept · `hploy down %s --volumes` also deletes the data", app))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	cmd.Flags().BoolVarP(&withVolumes, "volumes", "v", false, "also delete the volumes and their data")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip the confirmation")
	return cmd
}

func cmdStop() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "stop [app]",
		Short: "Stop an app's containers without removing them",
		Long: "The containers stay, so plan reports them as needing a start rather\n" +
			"than a rebuild.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appName(args, file)
			if err != nil {
				return err
			}
			eng, err := dockerEngine(cmd.Context())
			if err != nil {
				return err
			}
			containers, _, _, err := ownedBy(cmd.Context(), eng, app)
			if err != nil {
				return err
			}

			nameW := 0
			for _, c := range containers {
				if c.State == "running" {
					nameW = max(nameW, len(c.Name))
				}
			}

			stopped := 0
			for _, c := range containers {
				if c.State != "running" {
					continue
				}
				if err := eng.Stop(cmd.Context(), c.Name, 10*time.Second); err != nil {
					return err
				}
				// Not the red "-" of a removal: these containers are still here.
				fmt.Printf("  %s %s  %s  %s\n", ui.Warn.Render("■"),
					ui.Dim.Render("container"), c.Name+pad(nameW-len(c.Name)),
					ui.Dim.Render("stopped"))
				stopped++
			}
			if stopped == 0 {
				fmt.Println(ui.Note("nothing of app %q is running", app))
				return nil
			}
			fmt.Println()
			fmt.Println(ui.Note("containers kept · `hploy apply` starts them again"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	return cmd
}

// appName takes the app from the argument, or from the spec in the current
// directory. Tearing down must not require a readable spec: the argument form
// still works after the file is gone.
func appName(args []string, file string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if _, err := os.Stat(file); err != nil {
		return "", fmt.Errorf("no spec here and no app named — try `hploy down <app>` (see `hploy apps`)")
	}
	sp, err := spec.Load(file)
	if err != nil {
		return "", err
	}
	return sp.App, nil
}

// ownedBy collects what carries this app's label. Nothing else is ever
// returned, so nothing else can be removed.
func ownedBy(ctx context.Context, eng *engine.Client, app string) ([]engine.Container, string, []string, error) {
	all, err := eng.List(ctx, true)
	if err != nil {
		return nil, "", nil, err
	}
	var containers []engine.Container
	for _, c := range all {
		if c.App == app {
			containers = append(containers, c)
		}
	}

	networks, err := eng.ListNetworks(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	network := ""
	for _, n := range networks {
		if n.App == app {
			network = n.Name
		}
	}

	vols, err := eng.ListVolumes(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	var volumes []string
	for _, v := range vols {
		if v.App == app {
			volumes = append(volumes, v.Name)
		}
	}
	return containers, network, volumes, nil
}
