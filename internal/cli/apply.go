package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hercilioln/hploy/internal/plan"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdApply() *cobra.Command {
	var file string
	var assumeYes, quiet bool

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
			err = plan.Execute(cmd.Context(), eng, t, p, plan.Output{
				Step: stepPrinter(),
				Logs: buildLogWriter(quiet),
			})
			if err != nil {
				return err
			}
			fmt.Println()
			fmt.Println(ui.Good.Render("done"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", defaultSpecFile, "path to the spec")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip the confirmation")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "hide the build output")
	return cmd
}

// stepPrinter is the one-line-per-action reporter shared by apply and build.
func stepPrinter() plan.Progress {
	return func(verb plan.Verb, kind plan.Kind, name, note string) {
		line := fmt.Sprintf("  %s %s %s", verbMark(verb), ui.Dim.Render(string(kind)), name)
		if note != "" {
			line += "  " + ui.Dim.Render(note)
		}
		fmt.Println(line)
	}
}

// buildLogWriter indents the daemon's output so it reads as a nested detail
// rather than competing with hploy's own lines.
func buildLogWriter(quiet bool) io.Writer {
	if quiet {
		return nil
	}
	return &indenter{w: os.Stdout, prefix: "      "}
}

type indenter struct {
	w       io.Writer
	prefix  string
	midLine bool
}

func (i *indenter) Write(p []byte) (int, error) {
	for _, line := range strings.SplitAfter(string(p), "\n") {
		if line == "" {
			continue
		}
		text, complete := strings.CutSuffix(line, "\n")

		// The daemon sends a bare "\n" as its own frame; styling an empty
		// string would emit colour codes around nothing.
		if text != "" {
			if !i.midLine {
				io.WriteString(i.w, i.prefix)
			}
			io.WriteString(i.w, ui.Dim.Render(text))
		}
		if complete {
			io.WriteString(i.w, "\n")
		}
		i.midLine = !complete
	}
	return len(p), nil
}

func confirm() (bool, error) {
	return confirmWith("\napply these changes? [y/N] ")
}

func confirmWith(prompt string) (bool, error) {
	fmt.Print(prompt)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		// No stdin (a pipe, or cron): treat silence as no. An apply must never
		// happen because nobody was there to say no.
		return false, nil
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}
