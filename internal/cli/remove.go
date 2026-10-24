package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdRemove() *cobra.Command {
	var assumeYes bool

	cmd := &cobra.Command{
		Use:     "remove <app>",
		Aliases: []string{"rm"},
		Short:   "Unregister an app from this machine",
		Long: "Deletes the app's entry in the hploy store. The clone is never touched:\n" +
			"hploy did not create it, its .env holds secrets that exist nowhere\n" +
			"else, and nothing here is worth an irreversible rm -rf of someone's\n" +
			"working directory.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := store.LoadApp(args[0])
			if err != nil {
				return err
			}
			return removeApp(cmd, app, assumeYes)
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip the confirmation")
	return cmd
}

// removeApp is the command's body, so the apps menu can reach it too.
func removeApp(cmd *cobra.Command, app *store.App, assumeYes bool) error {
	// What is still on the machine decides whether this is safe.
	// Unregistering a running app would leave containers labelled for a name
	// that hploy no longer knows -- orphans that `down` could not reach any
	// more.
	eng, err := dockerEngine(cmd.Context())
	if err != nil {
		return err
	}
	containers, network, volumes, err := ownedBy(cmd.Context(), eng, app.Name)
	if err != nil {
		return err
	}
	if len(containers) > 0 || network != "" {
		return stillRunning(app.Name, containers, network)
	}

	fmt.Println()
	fmt.Println(ui.Title.Render("remove " + app.Name))
	fmt.Println()
	reportLeftovers(app, volumes)

	if !assumeYes {
		ok, err := confirmWith("unregister " + app.Name + "?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("cancelled")
			return nil
		}
	}

	if err := unregister(app); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println(ui.Good.Render("removed ") + app.Name + " " + dim("from "+store.Dir()))
	return nil
}

// unregister deletes the store entry and nothing else.
//
// The clone stays. hploy did not create it, its .env holds secrets that exist
// nowhere else because git ignores them, and no amount of "remove from hploy"
// justifies an irreversible rm -rf of someone's working directory. The code is
// in git; that file is not.
func unregister(app *store.App) error {
	return os.RemoveAll(store.AppDir(app.Name))
}

// stillRunning refuses rather than orphaning what is on the machine.
func stillRunning(app string, containers []engine.Container, network string) error {
	var what []string
	if n := len(containers); n > 0 {
		what = append(what, fmt.Sprintf("%d container(s)", n))
	}
	if network != "" {
		what = append(what, "a network")
	}
	return fmt.Errorf("%s still has %s on this machine\n"+
		"  unregistering now would leave them with nobody to remove them\n"+
		"  bring it down first:  hploy down %s", app, strings.Join(what, " and "), app)
}

// reportLeftovers names everything that outlives the registration, because
// after this the app's own paths are the only record of where they were.
func reportLeftovers(app *store.App, volumes []string) {
	fmt.Println("  " + dim("this deletes") + "  " + store.AppDir(app.Name))

	if inStore(app) {
		fmt.Println()
		fmt.Println("  " + ui.Warn.Render("the spec lives in the store and is not committed anywhere"))
		fmt.Println("  " + dim("removing the app deletes the only copy — keep it first if you want it:"))
		fmt.Println()
		fmt.Printf("    cp %s %s\n", app.SpecPath(), filepath.Join(app.Path, "hploy.yaml"))
	}

	fmt.Println()
	fmt.Println("  " + dim("this is kept"))
	fmt.Println("    " + dim("clone    ") + app.Path)
	for _, v := range volumes {
		fmt.Println("    " + dim("volume   ") + v)
	}
	if len(volumes) == 0 {
		fmt.Println("    " + dim("volumes  ") + dim("none"))
	}
	fmt.Println()

	fmt.Println("  " + dim("the clone is yours — hploy never created it and its .env is not in git."))
	fmt.Println("  " + dim("delete it yourself if you want it gone:"))
	fmt.Printf("    rm -rf %s\n", app.Path)

	if len(volumes) > 0 {
		// Said before the confirmation, because afterwards hploy no longer
		// knows this app and that command would not resolve the name.
		fmt.Println()
		fmt.Println("  " + ui.Warn.Render("the data outlives the registration"))
		fmt.Println("  " + dim("adding "+app.Name+" again reattaches it. to delete it instead, answer no"))
		fmt.Println("  " + dim("and run this first:"))
		fmt.Println()
		fmt.Printf("    hploy down --volumes %s\n", app.Name)
	}
	fmt.Println()
}

// inStore reports a generated spec that was never copied into the repo.
func inStore(a *store.App) bool {
	return filepath.Dir(a.SpecPath()) != a.Path
}
