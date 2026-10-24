// Package store holds hploy's local state and reads env files.
package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dir is ~/.hploy, overridable with HPLOY_HOME.
func Dir() string {
	if d := os.Getenv("HPLOY_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".hploy"
	}
	return filepath.Join(home, ".hploy")
}

func AppDir(app string) string { return filepath.Join(Dir(), "apps", app) }

// HployEnvPath holds secrets the application must not see, such as a registry
// password or a webhook secret. Usually absent.
func HployEnvPath(app string) string { return filepath.Join(AppDir(app), "env") }

// LoadFile reads a KEY=value file. A missing file yields an empty map, not an
// error: plan reports what is missing with better wording than a stat error.
func LoadFile(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected KEY=value", path, line)
		}
		out[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
	}
	return out, sc.Err()
}

// AppEnv returns both halves of the environment.
//
// lookup is what ${NAME} resolves against: the application's file plus hploy's
// own store, for keys the application does not carry. fileEnv is only the
// application's file, and is what env.from_file injects -- hploy's store may
// hold its own secrets, which must not be handed to the app container.
func AppEnv(specDir, envFile, app string) (lookup, fileEnv map[string]string, err error) {
	fileEnv, err = LoadFile(filepath.Join(specDir, envFile))
	if err != nil {
		return nil, nil, err
	}
	extra, err := LoadFile(HployEnvPath(app))
	if err != nil {
		return nil, nil, err
	}

	lookup = make(map[string]string, len(fileEnv)+len(extra))
	for k, v := range extra {
		lookup[k] = v
	}
	// The application's file wins: it is the documented source of truth.
	for k, v := range fileEnv {
		lookup[k] = v
	}
	return lookup, fileEnv, nil
}

// unquote strips surrounding quotes. Nothing else is interpreted, so a value
// is taken literally -- a "$" in a password stays a "$".
func unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	first, last := v[0], v[len(v)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
