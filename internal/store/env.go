package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Entry is one variable. A slice of them carries the order, which a map
// cannot: an env file's order is how its author grouped the variables.
type Entry struct {
	Key   string
	Value string
}

// EnvOrder lists the keys a file declares, in file order. A missing file has
// no keys, which is not an error.
func EnvOrder(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		key, ok := envKeyOf(line)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out, nil
}

// SaveEnv updates the file in place, line by line.
//
// A key already in the file keeps its position, its comment above it and an
// "export" prefix if it had one; only the value changes. A key declared on
// more than one line has every one of them updated. New keys are appended. Lines for keys that entries does not mention are left exactly as
// they were -- this file is the application's, not hploy's, and rewriting it
// from a map would throw away every comment in it.
func SaveEnv(path string, entries []Entry) error {
	want := make(map[string]string, len(entries))
	for _, e := range entries {
		want[e.Key] = e.Value
	}

	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var b strings.Builder
	written := map[string]bool{}

	if len(old) == 0 {
		b.WriteString("# Written by hploy. Never commit this file.\n")
	} else {
		lines := strings.Split(string(old), "\n")
		// A trailing newline yields a final empty element; dropping it keeps
		// the file from growing a blank line on every save.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, line := range lines {
			key, ok := envKeyOf(line)
			// Every line declaring the key, not only the first. LoadFile is
			// last-wins, so a duplicate left behind would hand back the old
			// value from a file that just reported itself saved.
			if value, replace := want[key]; ok && replace {
				written[key] = true
				prefix := ""
				if strings.HasPrefix(strings.TrimSpace(line), "export ") {
					prefix = "export "
				}
				b.WriteString(prefix + key + "=" + quoteEnv(value) + "\n")
				continue
			}
			b.WriteString(line + "\n")
		}
	}

	var added []Entry
	for _, e := range entries {
		if !written[e.Key] {
			added = append(added, e)
		}
	}
	if len(added) > 0 {
		if len(old) > 0 {
			b.WriteString("\n# Added by hploy.\n")
		}
		for _, e := range added {
			b.WriteString(e.Key + "=" + quoteEnv(e.Value) + "\n")
		}
	}

	return writeAtomic(path, b.String())
}

// envKeyOf returns the variable a line declares. Comments and blank lines
// declare nothing.
func envKeyOf(line string) (string, bool) {
	text := strings.TrimSpace(line)
	if text == "" || strings.HasPrefix(text, "#") {
		return "", false
	}
	text = strings.TrimPrefix(text, "export ")
	key, _, ok := strings.Cut(text, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", false
	}
	return key, true
}

// quoteEnv quotes only when the value would not survive being read back:
// LoadFile strips one layer of surrounding quotes and interprets nothing
// else, so a password keeps every character it has.
func quoteEnv(v string) string {
	if v == "" || !strings.ContainsAny(v, " \t#'\"") && !isQuoted(v) {
		return v
	}
	if strings.Contains(v, `"`) && !strings.Contains(v, "'") {
		return "'" + v + "'"
	}
	return `"` + v + `"`
}

// isQuoted reports a value that already begins and ends with a quote, which
// would be stripped on the way back in.
func isQuoted(v string) bool {
	if len(v) < 2 {
		return false
	}
	first, last := v[0], v[len(v)-1]
	return (first == '"' && last == '"') || (first == '\'' && last == '\'')
}

// writeAtomic replaces the file through a rename, so an interrupted save
// cannot leave a half-written env file behind.
func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hploy-env-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
