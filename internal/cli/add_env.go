package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hercilioln/hploy/internal/detect"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
)

// Names that usually hold a credential. Used only to decide whether the panel
// masks the value.
var reSecretName = regexp.MustCompile(`(?i)(PASSWORD|PASSWD|SECRET|TOKEN|_KEY|^KEY$|APIKEY|API_KEY|CREDENTIAL|PRIVATE)`)

// fillEnv opens the env panel at the end of `add`, so the app is configured
// in one place instead of through a question per variable.
//
// A refusal here is not a failure: the app is already registered and
// `hploy env` reopens the same panel whenever its values change.
func fillEnv(p *prompter, cfg *store.App, sp *spec.Spec, project *detect.Project) (int, error) {
	envPath := filepath.Join(cfg.Path, sp.EnvFileOrDefault())
	fields, err := envFields(sp, project, envPath)
	if err != nil {
		return 0, err
	}
	if len(fields) == 0 {
		return 0, nil
	}

	fmt.Println()
	fmt.Println("  " + ui.Header.Render("VALUES"))
	fmt.Println("  " + dim(fmt.Sprintf("%d variables, written to %s", len(fields), envPath)))
	if project.EnvExample != "" {
		fmt.Println("  " + dim("the values "+project.EnvExample+" already suggests are filled in"))
	}

	now, err := p.yesNo("fill them in now?", true)
	// The spec is already written at this point, so esc here means "not now"
	// rather than a step back into questions that have been answered.
	if errors.Is(err, ui.ErrBack) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !now {
		return 0, nil
	}

	saved, err := editEnv(envPath, "env "+sp.App, fields)
	if errors.Is(err, ui.ErrCancelled) {
		return 0, nil
	}
	return saved, err
}

var reRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func referencedNames(raw string) []string {
	var out []string
	for _, m := range reRef.FindAllStringSubmatch(raw, -1) {
		out = append(out, m[1])
	}
	return out
}

// hostHints maps the env key an application reads to the service it should
// point at, recognising a service by its image.
func hostHints(sp *spec.Spec) map[string]string {
	out := map[string]string{}
	for name, sv := range sp.Services {
		for _, pr := range presets {
			if pr.HostVar == "" {
				continue
			}
			base, _, _ := strings.Cut(pr.Image, ":")
			if strings.HasPrefix(sv.Image, base) {
				out[pr.HostVar] = name
			}
		}
	}
	return out
}

func summarize(cfg *store.App, sp *spec.Spec, project *detect.Project, generated bool, written int) {
	envPath := filepath.Join(cfg.Path, sp.EnvFileOrDefault())

	fmt.Println()
	fmt.Println(ui.Good.Render("registered ") + ui.Ours.Render(cfg.Name))
	fmt.Println()

	fmt.Println("  " + dim("config  ") + store.AppConfigPath(cfg.Name))
	fmt.Println("  " + dim("spec    ") + cfg.SpecPath())
	if written > 0 {
		fmt.Println("  " + dim("env     ") + envPath + dim(fmt.Sprintf(" (%d variables)", written)))
	}
	fmt.Println()

	if generated {
		fmt.Println("  " + ui.Warn.Render("the spec lives in the store, not in the repo"))
		fmt.Println("  " + dim("copy it into the project and commit it when you can — then a git pull"))
		fmt.Println("  " + dim("is all it takes to change the deployment:"))
		fmt.Println()
		fmt.Printf("    cp %s %s\n", cfg.SpecPath(), filepath.Join(cfg.Path, "hploy.yaml"))
		fmt.Println()
	}
	if project.Repo != "" && !project.EnvIgnored {
		fmt.Println("  " + ui.Bad.Render("! .env is not ignored by git — add it to .gitignore now"))
		fmt.Println()
	}

	fmt.Println("  " + dim("next"))
	if written == 0 {
		fmt.Printf("    hploy env %s\n", cfg.Name)
	}
	fmt.Printf("    hploy plan %s\n", cfg.Name)
	fmt.Printf("    hploy apply %s\n", cfg.Name)
}
