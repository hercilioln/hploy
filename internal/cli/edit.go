package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdEdit() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "edit [app]",
		Short: "Open the app's spec in $EDITOR",
		Long: "Edits hploy.yaml and refuses to keep a version that does not parse or\n" +
			"does not validate: a broken spec in the store would fail the next\n" +
			"apply instead of this edit.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := specPathFor(args, file)
			if err != nil {
				return err
			}
			return editSpec(path)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	return cmd
}

// specPathFor resolves which hploy.yaml to open, the same three ways the
// other commands resolve an app.
func specPathFor(args []string, file string) (string, error) {
	t, err := resolveTarget(args, file)
	if err != nil {
		return "", err
	}
	if t.Registered {
		app, err := store.FindByPath(t.Dir)
		if err != nil {
			return "", err
		}
		if app != nil {
			return app.SpecPath(), nil
		}
	}
	if len(args) > 0 {
		app, err := store.LoadApp(args[0])
		if err != nil {
			return "", err
		}
		return app.SpecPath(), nil
	}
	return filepath.Join(t.Dir, defaultSpecFile), nil
}

// editSpec reopens the editor until the file is valid or someone gives up, so
// a typo costs another pass rather than a failed deploy.
func editSpec(path string) error {
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	for {
		if err := runEditor(path); err != nil {
			return err
		}

		after, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(after) == string(before) {
			fmt.Println(ui.Note("unchanged"))
			return nil
		}

		sp, err := spec.Load(path)
		if err == nil {
			fmt.Println(ui.Good.Render("saved ") + path)
			fmt.Println()
			fmt.Println("  " + dim("the containers keep the old config until they are recreated"))
			fmt.Printf("    hploy plan %s\n", sp.App)
			return nil
		}

		fmt.Println()
		fmt.Println(ui.Bad.Render("the spec is not valid:"))
		for _, line := range strings.Split(err.Error(), "\n") {
			fmt.Println("  " + line)
		}
		fmt.Println()

		// Nobody to ask. Reverting is the safe end of this branch: a spec
		// that does not validate would fail the next apply instead.
		if !isTTY() {
			if werr := os.WriteFile(path, before, 0o600); werr != nil {
				return werr
			}
			return fmt.Errorf("reverted %s — no terminal to ask what to do: %w", path, err)
		}

		again, qerr := ui.Select(ui.Choice{
			Label:   "what now?",
			Options: []string{"edit it again", "keep it anyway", "undo my changes"},
		})
		if errors.Is(qerr, ui.ErrCancelled) {
			again = 2
		} else if qerr != nil {
			return qerr
		}

		switch again {
		case 0:
			continue
		case 1:
			// Their file, their call -- but plan and apply will refuse it.
			fmt.Println(ui.Warn.Render("kept — plan and apply will refuse it until it validates"))
			return nil
		default:
			if err := os.WriteFile(path, before, 0o600); err != nil {
				return err
			}
			fmt.Println(ui.Note("reverted %s", path))
			return nil
		}
	}
}

// runEditor prefers $VISUAL, then $EDITOR, then whatever is installed. It
// runs on the terminal, so the editor owns the screen until it exits.
func runEditor(path string) error {
	name := firstSet(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if name == "" {
		for _, candidate := range []string{"nano", "vim", "vi"} {
			if _, err := exec.LookPath(candidate); err == nil {
				name = candidate
				break
			}
		}
	}
	if name == "" {
		return fmt.Errorf("no editor found — set $EDITOR, or edit %s yourself", path)
	}

	// A command line, so EDITOR="code -w" works.
	fields := strings.Fields(name)
	cmd := exec.Command(fields[0], append(fields[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func firstSet(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
