package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/hercilioln/hploy/internal/detect"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func dim(s string) string { return ui.Dim.Render(s) }

func cmdAdd() *cobra.Command {
	var repo, branch string

	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Register an app on this machine",
		Long: "Asks for what cannot be read from the project, then writes the\n" +
			"deployment config to the store. The app's own directory is never\n" +
			"modified except for its .env, which git ignores -- so nothing has to\n" +
			"be committed from production.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runAdd(args, repo, branch)
			if errors.Is(err, ui.ErrCancelled) || errors.Is(err, ui.ErrBack) {
				// Ctrl+C, or esc at the very first question, leaves nothing
				// registered.
				fmt.Println()
				fmt.Println(ui.Note("cancelled — nothing was registered"))
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "clone this repository into the path first")
	cmd.Flags().StringVar(&branch, "branch", "", "branch to clone")
	return cmd
}

func runAdd(args []string, repo, branch string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	p, err := newPrompter()
	if err != nil {
		return err
	}

	if repo != "" {
		if err := clone(repo, dir, branch); err != nil {
			return err
		}
	}

	project, err := detect.Inspect(dir)
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	reportFindings(project)

	// What is already decided is not a question. Leaving the answered ones
	// out of the wizard keeps going back meaningful: every step it can land
	// on is one someone can actually change.
	app, err := committedAppName(project)
	if err != nil {
		return err
	}
	var domains []string
	// The spec in the repo wins. Only when there is none does hploy generate
	// one, and it goes in the store so the clone stays a clean mirror of git.
	generated := ""

	var steps []func() error
	if app == "" {
		steps = append(steps, func() error {
			name, err := askAppName(p, project)
			if err != nil {
				return err
			}
			app = name
			return nil
		})
	}
	steps = append(steps, func() error {
		list, err := askDomains(p, domains)
		if err != nil {
			return err
		}
		domains = list
		return nil
	})
	if !project.HasSpec {
		steps = append(steps, func() error {
			yaml, err := generateSpec(p, app, project)
			if err != nil {
				return err
			}
			generated = yaml
			return nil
		})
	}

	if err := p.wizard(steps...); err != nil {
		return err
	}

	cfg := &store.App{
		Name: app, Domains: domains, Path: project.Dir,
		Repo: project.Repo, Branch: project.Branch,
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(store.AppDir(app), 0o700); err != nil {
		return err
	}
	if generated != "" {
		path := filepath.Join(store.AppDir(app), "hploy.yaml")
		if err := os.WriteFile(path, []byte(generated), 0o600); err != nil {
			return err
		}
	}

	// Validate before registering, so a bad spec is not left behind under a
	// registered name.
	sp, err := spec.Load(cfg.SpecPath())
	if err != nil {
		return err
	}
	if sp.App != app {
		return fmt.Errorf("%s declares app %q, not %q", cfg.SpecPath(), sp.App, app)
	}

	// Registered before the values are asked for: the env panel can be left
	// without answering, and losing the whole wizard over that would be
	// worse than an app with no values yet.
	if err := store.SaveApp(cfg); err != nil {
		return err
	}
	written, err := fillEnv(p, cfg, sp, project)
	if err != nil {
		return err
	}

	summarize(cfg, sp, project, generated != "", written)
	return nil
}

func clone(repo, dir, branch string) error {
	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repo, dir)

	fmt.Println("  " + dim("cloning "+repo))
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, out)
	}
	return nil
}

// reportFindings prints before the wizard starts, so its lines are outside
// what going back can erase.
func reportFindings(p *detect.Project) {
	fmt.Println(ui.Title.Render("hploy add"))
	fmt.Println()
	fmt.Println("  " + dim(p.Dir))
	fmt.Println()

	found := func(ok bool, yes, no string) {
		if ok {
			fmt.Println("  " + ui.Good.Render("✓") + " " + yes)
			return
		}
		fmt.Println("  " + dim("·") + " " + dim(no))
	}

	found(p.Repo != "", "git "+p.Repo+" "+dim("("+p.Branch+")"), "not a git clone — hploy deploy will have nothing to pull")
	if p.Dockerfile != "" {
		ports := ""
		if len(p.Ports) > 0 {
			ports = dim(fmt.Sprintf("(EXPOSE %s)", joinInts(p.Ports)))
		}
		fmt.Println("  " + ui.Good.Render("✓") + " " + p.Dockerfile + " " + ports)
	} else {
		fmt.Println("  " + dim("· no Dockerfile — the image has to come from a registry"))
	}
	found(p.HasSpec, "hploy.yaml in the repo — using it as is", "no hploy.yaml — one will be generated into the store")
	if p.EnvExample != "" {
		fmt.Println("  " + ui.Good.Render("✓") + " " + p.EnvExample + " " +
			dim(fmt.Sprintf("(%d variables)", len(p.EnvKeys))))
	}
	if p.HasCompose {
		fmt.Println("  " + ui.Warn.Render("!") + " a compose file is present; hploy does not read it")
	}
	if p.HasEnv && p.Repo != "" && !p.EnvIgnored {
		fmt.Println("  " + ui.Bad.Render("!") + " .env is NOT in .gitignore — add it before committing")
	}
	fmt.Println()
}

func joinInts(ns []int) string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, strconv.Itoa(n))
	}
	return strings.Join(out, ", ")
}

var reSlug = regexp.MustCompile(`[^a-z0-9-]+`)

// committedAppName is the name a spec in the repo already settled, or "".
// It has to agree, so there is nothing to ask.
func committedAppName(project *detect.Project) (string, error) {
	if !project.HasSpec {
		return "", nil
	}
	sp, err := spec.Load(filepath.Join(project.Dir, "hploy.yaml"))
	if err != nil {
		return "", nil
	}
	// The name is not negotiable here, so a collision cannot be answered
	// with a different one. Refusing beats repointing a registered app at
	// another directory, which would make the next apply deploy the wrong
	// clone.
	if err := notRegistered(sp.App); err != nil {
		return "", err
	}
	fmt.Println("  " + dim("app name from the repo's spec: ") + sp.App)
	return sp.App, nil
}

// notRegistered refuses a name the store already holds.
func notRegistered(name string) error {
	if _, err := os.Stat(store.AppConfigPath(name)); err == nil {
		return fmt.Errorf("%q is already registered — remove it first or pick another name", name)
	}
	return nil
}

func askAppName(p *prompter, project *detect.Project) (string, error) {
	def := strings.Trim(reSlug.ReplaceAllString(strings.ToLower(filepath.Base(project.Dir)), "-"), "-")
	return p.askValid("app name", def, func(s string) error {
		cfg := store.App{Name: s, Path: project.Dir}
		if err := cfg.Validate(); err != nil {
			return err
		}
		return notRegistered(s)
	})
}

func askDomains(p *prompter, current []string) ([]string, error) {
	answer, err := p.askValid("domains "+dim("(comma separated, empty for none)"),
		strings.Join(current, ","), func(s string) error {
			_, err := parseDomains(s)
			return err
		})
	if err != nil {
		return nil, err
	}
	return parseDomains(answer)
}

func parseDomains(s string) ([]string, error) {
	var out []string
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		clean, err := store.CleanDomain(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, clean)
	}
	return out, nil
}
