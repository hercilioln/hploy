package cli

import (
	"fmt"
	"strings"

	"github.com/hercilioln/hploy/internal/plan"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdPlan() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "plan [app]",
		Short: "Show what would change to match the spec",
		Long: "Compares the spec with the machine and prints the actions. Changes\n" +
			"nothing. Reads the current directory, or a registered app by name.",
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
			p, err := plan.Build(cmd.Context(), eng, t)
			if err != nil {
				return err
			}
			fmt.Print(renderPlan(p))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	return cmd
}

func renderPlan(p *plan.Plan) string {
	out := ui.Title.Render("app "+p.App) + "\n\n"

	// Column widths come from the actions themselves, so the detail column
	// lines up whatever the names are.
	var todo []plan.Action
	kindW, nameW := 0, 0
	for _, a := range p.Actions {
		if a.Verb == plan.Unchanged {
			continue
		}
		todo = append(todo, a)
		kindW = max(kindW, len(a.Kind))
		nameW = max(nameW, len(a.Name))
	}

	for _, a := range todo {
		// Pad the plain text and style afterwards: a styled string carries
		// escape codes, and padding by byte length would misalign the column.
		line := fmt.Sprintf("  %s %s  %s%s",
			verbMark(a.Verb),
			ui.Dim.Render(string(a.Kind))+pad(kindW-len(a.Kind)),
			a.Name+pad(nameW-len(a.Name)),
			planDetail(a))
		out += strings.TrimRight(line, " ") + "\n"
	}
	if len(todo) == 0 {
		out += "  " + ui.Good.Render("nothing to do") + "\n"
	}

	out += "\n" + ui.Note("%d to create · %d to recreate · %d to start · %d to remove · %d unchanged",
		p.Count(plan.Create), p.Count(plan.Recreate), p.Count(plan.Start),
		p.Count(plan.Remove), p.Count(plan.Unchanged)) + "\n"

	for _, w := range p.Warnings {
		out += "\n" + ui.Warn.Render("warning:") + " " + w + "\n"
	}

	if len(p.Problems) > 0 {
		out += "\n" + ui.Bad.Render("blocking:") + "\n"
		for _, pr := range p.Problems {
			out += "  " + ui.Bad.Render("!") + " " + pr + "\n"
		}
		out += "\n" + ui.Note("the app env file is where these belong") + "\n"
	}
	return out
}

func verbMark(v plan.Verb) string {
	switch v {
	case plan.Create:
		return ui.Good.Render("+")
	case plan.Recreate:
		return ui.Warn.Render("~")
	case plan.Start:
		return ui.Warn.Render("▸")
	case plan.Remove:
		return ui.Bad.Render("-")
	default:
		return ui.Dim.Render("=")
	}
}

func planDetail(a plan.Action) string {
	s := ""
	if a.Detail != "" {
		s += "  " + ui.Dim.Render(a.Detail)
	}
	if a.Reason != "" {
		s += "  " + ui.Dim.Render("("+a.Reason+")")
	}
	return s
}

func pad(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}
