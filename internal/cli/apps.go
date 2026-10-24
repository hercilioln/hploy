package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdApps() *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "List the apps registered on this machine",
		Long: "The registry is the store directory itself: one folder per app. There\n" +
			"is no index file to fall out of sync.\n\n" +
			"On a terminal it then offers what can be done to one of them. Piped or\n" +
			"with --list it only prints the table, so a script sees the same output\n" +
			"it always did.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			apps, err := store.ListApps()
			if err != nil {
				return err
			}
			if len(apps) == 0 {
				fmt.Println(ui.Note("no apps registered in %s", store.Dir()))
				return nil
			}
			fmt.Print(appsTable(apps))

			if list || !isTTY() {
				return nil
			}
			err = appsMenu(cmd, apps)
			if errors.Is(err, ui.ErrCancelled) {
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "only print the table, without the menu")
	return cmd
}

func appsTable(apps []*store.App) string {
	t := ui.NewTable("app", "domains", "spec", "path")
	for _, a := range apps {
		domains := ui.Dim.Render("—")
		if len(a.Domains) > 0 {
			domains = strings.Join(a.Domains, ", ")
		}
		t.Row(ui.Ours.Render(a.Name), domains, specOrigin(a), ui.Dim.Render(a.Path))
	}
	return t.String()
}

// appsMenu picks an app and then what to do to it. Only the two things that
// cannot be done anywhere else are here -- editing the spec and the domains;
// the rest of the commands take an app name and are better typed.
func appsMenu(cmd *cobra.Command, apps []*store.App) error {
	names := make([]string, 0, len(apps)+1)
	for _, a := range apps {
		names = append(names, a.Name)
	}
	names = append(names, "done")

	fmt.Println()
	choice, err := ui.Select(ui.Choice{Label: "edit which?", Options: names, Default: len(names) - 1})
	if err != nil || choice == len(apps) {
		return err
	}
	app := apps[choice]

	actions := []string{
		"spec      " + app.SpecPath(),
		"domains   " + strings.Join(app.Domains, ", "),
		"env       " + filepath.Join(app.Path, ".env"),
		"remove    unregister " + app.Name,
		"done",
	}
	if len(app.Domains) == 0 {
		actions[1] = "domains   " + ui.Dim.Render("none")
	}

	what, err := ui.Select(ui.Choice{Label: app.Name, Options: actions, Back: true})
	if errors.Is(err, ui.ErrBack) {
		return appsMenu(cmd, apps)
	}
	if err != nil {
		return err
	}

	switch what {
	case 0:
		return editSpec(app.SpecPath())
	case 1:
		return editDomains(app)
	case 2:
		return runEnvPanel(app)
	case 3:
		return removeApp(cmd, app, false)
	}
	return nil
}

func editDomains(app *store.App) error {
	p, err := newPrompter()
	if err != nil {
		return err
	}
	domains, err := askDomains(p, app.Domains)
	if errors.Is(err, ui.ErrBack) || errors.Is(err, ui.ErrCancelled) {
		return nil
	}
	if err != nil {
		return err
	}

	app.Domains = domains
	if err := app.Validate(); err != nil {
		return err
	}
	if err := store.SaveApp(app); err != nil {
		return err
	}

	shown := ui.Dim.Render("none")
	if len(domains) > 0 {
		shown = strings.Join(domains, ", ")
	}
	fmt.Println(ui.Good.Render("saved ") + app.Name + " " + dim("domains: ") + shown)
	fmt.Println()
	fmt.Println("  " + dim("the proxy labels change with the container"))
	fmt.Printf("    hploy apply %s\n", app.Name)
	return nil
}

// specOrigin says whether the spec is the one committed in the repo or the
// bootstrap copy in the store, since that decides what a git pull changes.
func specOrigin(a *store.App) string {
	if inStore(a) {
		return ui.Warn.Render("store")
	}
	return ui.Good.Render("repo")
}
