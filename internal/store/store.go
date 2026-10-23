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

// AppEnv is the env a spec resolves against: the application's file, plus
// hploy's own store for keys the application does not carry.
func AppEnv(specDir, envFile, app string) (map[string]string, error) {
	merged, err := LoadFile(filepath.Join(specDir, envFile))
	if err != nil {
		return nil, err
	}
	extra, err := LoadFile(HployEnvPath(app))
	if err != nil {
		return nil, err
	}
	// The application's file wins: it is the documented source of truth.
	for k, v := range extra {
		if _, taken := merged[k]; !taken {
			merged[k] = v
		}
	}
	return merged, nil
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
