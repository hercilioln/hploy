package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/hercilioln/hploy/internal/detect"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdEnv() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "env [app]",
		Short: "Edit the app's environment variables",
		Long: "Opens the whole env file as one list: every variable the spec needs,\n" +
			"everything the project's example declares and everything already set,\n" +
			"with its current value. Only the file in the app's own directory is\n" +
			"touched, and git ignores it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveTarget(args, file)
			if err != nil {
				return err
			}
			return runEnv(t.Spec, t.Dir)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	return cmd
}

// runEnvPanel opens the panel for a registered app, which is what the apps
// menu reaches for.
func runEnvPanel(app *store.App) error {
	sp, err := spec.Load(app.SpecPath())
	if err != nil {
		return err
	}
	return runEnv(sp, app.Path)
}

func runEnv(sp *spec.Spec, dir string) error {
	if !isTTY() {
		return fmt.Errorf("this command opens an editor and stdin is not a terminal")
	}

	envPath := filepath.Join(dir, sp.EnvFileOrDefault())
	project, err := detect.Inspect(dir)
	if err != nil {
		return err
	}

	fields, err := envFields(sp, project, envPath)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		fmt.Println(ui.Note("nothing to set: the spec needs no variables and there is no env example"))
		return nil
	}

	saved, err := editEnv(envPath, "env "+sp.App, fields)
	if errors.Is(err, ui.ErrCancelled) {
		fmt.Println(ui.Note("nothing was written"))
		return nil
	}
	if err != nil {
		return err
	}
	if saved == 0 {
		fmt.Println(ui.Note("every value was left empty, so nothing was written"))
		return nil
	}

	fmt.Println(ui.Good.Render("saved ") + envPath + " " + dim(fmt.Sprintf("(%d variables)", saved)))
	fmt.Println()
	fmt.Println("  " + dim("the container holds the old values until it is recreated"))
	fmt.Printf("    hploy apply %s\n", sp.App)
	return nil
}

// envFields assembles the list the panel shows, in the order someone reads an
// env file: what is already set first, then what the project's example
// suggests, then whatever else the spec asks for.
//
// A value is never invented. It comes from the file, or from the example when
// the file has nothing -- which is the whole point of an example, and what
// made answering fifty questions in a row pointless.
func envFields(sp *spec.Spec, project *detect.Project, envPath string) ([]ui.Field, error) {
	current, err := store.LoadFile(envPath)
	if err != nil {
		return nil, err
	}
	order, err := store.EnvOrder(envPath)
	if err != nil {
		return nil, err
	}

	needed, required := specKeys(sp)
	hints := hostHints(sp)

	seen := map[string]bool{}
	var out []ui.Field
	add := func(key, note string) {
		if key == "" || seen[key] {
			return
		}
		seen[key] = true

		value, inFile := current[key]
		switch {
		case inFile:
			note = ""
		case project.EnvSample[key] != "":
			value = project.EnvSample[key]
			note = project.EnvExample
		case hints[key] != "":
			// A service name is its network alias, so this is the host the
			// container will actually reach it at.
			value = hints[key]
			note = "service"
		}

		out = append(out, ui.Field{
			Name:     key,
			Value:    value,
			Note:     note,
			Secret:   reSecretName.MatchString(key),
			Required: required[key],
		})
	}

	for _, k := range order {
		add(k, "")
	}
	for _, k := range project.EnvKeys {
		add(k, "")
	}
	for _, k := range needed {
		add(k, "spec")
	}
	return out, nil
}

// newEnvField is a variable someone adds by hand. It is masked on the same
// evidence as the rest: what the name looks like.
func newEnvField(name string) ui.Field {
	return ui.Field{Name: name, Note: "added", Secret: reSecretName.MatchString(name)}
}

// specKeys is what the spec cannot run without: the keys it declares required
// and the names its templates reference.
func specKeys(sp *spec.Spec) (needed []string, required map[string]bool) {
	required = map[string]bool{}
	for _, sv := range sp.StartOrder() {
		for _, k := range sv.Env.Required {
			required[k] = true
		}
		for _, raw := range sv.Env.Vars {
			for _, name := range referencedNames(raw) {
				required[name] = true
			}
		}
		if sv.Build != nil {
			for _, raw := range sv.Build.Args {
				for _, name := range referencedNames(raw) {
					required[name] = true
				}
			}
		}
	}

	for k := range required {
		needed = append(needed, k)
	}
	sort.Strings(needed)
	return needed, required
}

// editEnv runs the panel and writes what comes back.
func editEnv(envPath, title string, fields []ui.Field) (int, error) {
	done, err := ui.Form(fields, ui.FormOptions{
		Title:      title,
		Note:       envPath + " · never committed",
		Generators: generators,
		NewField:   newEnvField,
	})
	if err != nil {
		return 0, err
	}
	return saveFields(envPath, done)
}

// saveFields is the writing half, kept apart from the panel so it can be
// tested without a terminal.
func saveFields(envPath string, fields []ui.Field) (int, error) {
	existing, err := store.LoadFile(envPath)
	if err != nil {
		return 0, err
	}

	var entries []store.Entry
	for _, f := range fields {
		_, inFile := existing[f.Name]
		// An empty value that is not in the file is left out on purpose.
		// Writing "KEY=" would make plan see the variable as set, and a
		// missing APP_KEY would stop being reported.
		if f.Value == "" && !inFile {
			continue
		}
		entries = append(entries, store.Entry{Key: f.Name, Value: f.Value})
	}
	if len(entries) == 0 {
		return 0, nil
	}
	if err := store.SaveEnv(envPath, entries); err != nil {
		return 0, err
	}
	return len(entries), nil
}
