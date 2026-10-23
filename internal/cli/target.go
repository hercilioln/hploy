package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hercilioln/hploy/internal/plan"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
)

const defaultSpecFile = "hploy.yaml"

// resolveTarget accepts the two ways of naming an app.
//
// With a name, the deployment config comes from the store and the spec from
// the clone it points at. Without one, the current directory is read directly:
// the local test, where there is no domain and no proxy.
func resolveTarget(args []string, file string) (*plan.Target, error) {
	if len(args) == 0 {
		sp, env, err := loadSpec(file)
		if err != nil {
			return nil, err
		}
		return &plan.Target{Spec: sp, Env: env, Dir: filepath.Dir(file)}, nil
	}

	app, err := store.LoadApp(args[0])
	if err != nil {
		return nil, err
	}
	sp, err := spec.Load(app.SpecPath())
	if err != nil {
		return nil, err
	}
	if sp.App != app.Name {
		return nil, fmt.Errorf("%s declares app %q but it is registered as %q",
			app.SpecPath(), sp.App, app.Name)
	}
	env, err := store.AppEnv(app.Path, sp.EnvFileOrDefault(), app.Name)
	if err != nil {
		return nil, err
	}
	return &plan.Target{
		Spec:       sp,
		Env:        env,
		Dir:        app.Path,
		Domains:    app.Domains,
		Registered: true,
	}, nil
}

func loadSpec(file string) (*spec.Spec, map[string]string, error) {
	if _, err := os.Stat(file); err != nil {
		abs, _ := filepath.Abs(file)
		return nil, nil, fmt.Errorf("no spec at %s — run this from the app directory, pass -f, or name a registered app", abs)
	}
	sp, err := spec.Load(file)
	if err != nil {
		return nil, nil, err
	}
	env, err := store.AppEnv(filepath.Dir(file), sp.EnvFileOrDefault(), sp.App)
	if err != nil {
		return nil, nil, err
	}
	return sp, env, nil
}
