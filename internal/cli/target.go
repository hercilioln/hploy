package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hercilioln/hploy/internal/plan"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
)

const defaultSpecFile = "hploy.yaml"

// resolveTarget finds the app three ways, in this order:
//
//  1. named on the command line
//  2. registered at this directory -- its spec may live in the store rather
//     than in the repo, so the directory alone has to identify it
//  3. an hploy.yaml here, unregistered: the local test, with no domain
func resolveTarget(args []string, file string) (*plan.Target, error) {
	if len(args) > 0 {
		app, err := store.LoadApp(args[0])
		if err != nil {
			return nil, err
		}
		return registeredTarget(app)
	}

	// An explicit -f means that file and nothing else.
	if file == defaultSpecFile {
		app, err := store.FindByPath(filepath.Dir(file))
		if err != nil {
			return nil, err
		}
		if app != nil {
			return registeredTarget(app)
		}
	}

	sp, env, fileEnv, err := loadSpec(file)
	if err != nil {
		return nil, err
	}
	return &plan.Target{Spec: sp, Env: env, FileEnv: fileEnv, Dir: filepath.Dir(file)}, nil
}

func registeredTarget(app *store.App) (*plan.Target, error) {
	sp, err := spec.Load(app.SpecPath())
	if err != nil {
		return nil, err
	}
	if sp.App != app.Name {
		return nil, fmt.Errorf("%s declares app %q but it is registered as %q",
			app.SpecPath(), sp.App, app.Name)
	}
	env, fileEnv, err := store.AppEnv(app.Path, sp.EnvFileOrDefault(), app.Name)
	if err != nil {
		return nil, err
	}
	return &plan.Target{
		Spec:       sp,
		Env:        env,
		FileEnv:    fileEnv,
		Dir:        app.Path,
		Domains:    app.Domains,
		Registered: true,
	}, nil
}

func loadSpec(file string) (*spec.Spec, map[string]string, map[string]string, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, nil, nil, noAppHere(file)
	}
	sp, err := spec.Load(file)
	if err != nil {
		return nil, nil, nil, err
	}
	env, fileEnv, err := store.AppEnv(filepath.Dir(file), sp.EnvFileOrDefault(), sp.App)
	if err != nil {
		return nil, nil, nil, err
	}
	return sp, env, fileEnv, nil
}

// noAppHere says what to do next, listing what is registered rather than
// leaving someone to guess the name.
func noAppHere(file string) error {
	abs, _ := filepath.Abs(file)
	msg := fmt.Sprintf("no spec at %s, and this directory is not a registered app", abs)

	if names := store.Names(); len(names) > 0 {
		return fmt.Errorf("%s\n  registered here: %s\n  name one of them, or run `hploy add`",
			msg, strings.Join(names, ", "))
	}
	return fmt.Errorf("%s\n  run `hploy add` to register it", msg)
}
