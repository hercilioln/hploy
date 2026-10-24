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
	// FromFile injects every key of the application's own env file. This is
	// the usual shape for an application container, which wants its whole
	// configuration; a database gets named vars instead.
	FromFile bool `yaml:"from_file"`

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

// Resolve turns the declaration into the values to inject.
//
// lookup is what ${NAME} reads: the application's env file plus hploy's own
// store. fileEnv is only the application's file, and is the only thing
// FromFile injects -- the store may hold hploy's secrets, such as a webhook
// or registry password, which have no business inside the app container.
//
// An unknown reference is an error, not an empty string: silently substituting
// nothing produces a container with a blank password and no warning.
func (e Env) Resolve(lookup, fileEnv map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(e.Vars)+len(fileEnv))
	if e.FromFile {
		for k, v := range fileEnv {
			out[k] = v
		}
	}
	var missing []string

	keys := make([]string, 0, len(e.Vars))
	for k := range e.Vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// Vars are applied last, so an explicit declaration beats the file.
		value := reVar.ReplaceAllStringFunc(e.Vars[k], func(match string) string {
			name := reVar.FindStringSubmatch(match)[1]
			v, ok := lookup[name]
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
