package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Env is what a service needs from the environment.
//
// Values never live here. Vars carries templates resolved against the app's
// env file, so a password exists in one place only.
type Env struct {
	// Required keys must exist in the app env. Useful for variables the
	// application reads itself and hploy never injects, like APP_KEY.
	Required []string `yaml:"required"`

	// Vars are injected into the container. A value may be a literal or a
	// ${NAME} reference into the app env.
	Vars map[string]string `yaml:"vars"`
}

// Only ${NAME} interpolates. A bare "$" is literal, so a dollar sign inside a
// password stays a dollar sign.
var reVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Resolve turns Vars into the values to inject.
//
// An unknown reference is an error, not an empty string: silently substituting
// nothing produces a container with a blank password and no warning.
func (e Env) Resolve(appEnv map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(e.Vars))
	var missing []string

	keys := make([]string, 0, len(e.Vars))
	for k := range e.Vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		value := reVar.ReplaceAllStringFunc(e.Vars[k], func(match string) string {
			name := reVar.FindStringSubmatch(match)[1]
			v, ok := appEnv[name]
			if !ok {
				missing = append(missing, name)
				return ""
			}
			return v
		})
		out[k] = value
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("not set in the app env: %s", strings.Join(dedup(missing), ", "))
	}
	return out, nil
}

// Missing lists the Required keys absent from the app env.
func (e Env) Missing(appEnv map[string]string) []string {
	var out []string
	for _, k := range e.Required {
		if _, ok := appEnv[k]; !ok {
			out = append(out, k)
		}
	}
	// Names referenced by Vars count as required too.
	for _, raw := range e.Vars {
		for _, m := range reVar.FindAllStringSubmatch(raw, -1) {
			if _, ok := appEnv[m[1]]; !ok {
				out = append(out, m[1])
			}
		}
	}
	sort.Strings(out)
	return dedup(out)
}

func dedup(in []string) []string {
	out := in[:0:0]
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
