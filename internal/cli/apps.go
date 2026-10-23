package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdApps() *cobra.Command {
	return &cobra.Command{
		Use:   "apps",
		Short: "List the apps registered on this machine",
		Long: "The registry is the store directory itself: one folder per app. There\n" +
			"is no index file to fall out of sync.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			apps, err := store.ListApps()
			if err != nil {
				return err
			}
			if len(apps) == 0 {
				fmt.Println(ui.Note("no apps registered in %s", store.Dir()))
				return nil
			}

			t := ui.NewTable("app", "domains", "spec", "path")
			for _, a := range apps {
				domains := ui.Dim.Render("—")
				if len(a.Domains) > 0 {
					domains = strings.Join(a.Domains, ", ")
				}
				t.Row(ui.Ours.Render(a.Name), domains, specOrigin(a), ui.Dim.Render(a.Path))
			}
			fmt.Print(t)
			return nil
		},
	}
}

// specOrigin says whether the spec is the one committed in the repo or the
// bootstrap copy in the store, since that decides what a git pull changes.
func specOrigin(a *store.App) string {
	if filepath.Dir(a.SpecPath()) == a.Path {
		return ui.Good.Render("repo")
	}
	return ui.Warn.Render("store")
}
