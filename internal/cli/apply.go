package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/hercilioln/hploy/internal/plan"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdApply() *cobra.Command {
	var file string
	var assumeYes, verbose bool

	cmd := &cobra.Command{
		Use:   "apply [app]",
		Short: "Make the machine match the spec",
		Long: "Shows the same output as plan, asks for confirmation, then carries it\n" +
			"out. Only containers labelled for this app are ever touched.",
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

			if len(p.Problems) > 0 {
				return fmt.Errorf("refusing to apply: resolve the problems above first")
			}
			if p.Empty() {
				return nil
			}
			if !assumeYes {
				ok, err := confirm()
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println(ui.Note("cancelled"))
					return nil
				}
			}

			fmt.Println()
			out, logs := output(ui.NewConsole(os.Stdout), verbose)
			if err := plan.Execute(cmd.Context(), eng, t, p, out); err != nil {
				logs.dump()
				return err
			}
			fmt.Println()
			fmt.Println(ui.Good.Render("done"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip the confirmation")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show the full build output")
	return cmd
}

// output wires the three things that share the terminal: hploy's own step
// lines, the daemon's log stream and the spinner.
func output(console *ui.Console, verbose bool) (plan.Output, *buildLog) {
	logs := newBuildLog(console, verbose)
	return plan.Output{
		Step: func(verb plan.Verb, kind plan.Kind, name, note string) {
			line := fmt.Sprintf("  %s %s %s", verbMark(verb), ui.Dim.Render(string(kind)), name)
			if note != "" {
				line += "  " + ui.Dim.Render(note)
			}
			console.Printf("%s", line)
		},
		Busy: console.Busy,
		Logs: logs,
	}, logs
}

func confirm() (bool, error) {
	return confirmWith("apply these changes?")
}

func confirmWith(question string) (bool, error) {
	// No terminal (a pipe, or cron): silence is a no. An apply must never
	// happen because nobody was there to say no -- but it has to say that is
	// the reason, or a piped run looks like it was answered.
	if !isTTY() {
		fmt.Println()
		fmt.Println(ui.Note("not a terminal, so there is nobody to ask — pass --yes to go ahead"))
		return false, nil
	}
	fmt.Println()
	choice, err := ui.Select(ui.Choice{
		Label: question, Options: []string{"yes", "no"}, Default: 1,
	})
	if errors.Is(err, ui.ErrCancelled) {
		return false, nil
	}
	return choice == 0, err
}
