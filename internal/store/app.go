package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// App is the deployment config: what is true of this machine rather than of
// the application. It never belongs in the app's repository, so the clone on
// the VPS stays a clean mirror of git and nothing is ever pushed from
// production.
//
// The spec (hploy.yaml) is the opposite: a property of the application, equal
// on every machine, and versioned with the code.
type App struct {
	Name    string   `yaml:"app"`
	Domains []string `yaml:"domains"`

	// Path is the clone hploy deploys from.
	Path   string `yaml:"path"`
	Repo   string `yaml:"repo,omitempty"`
	Branch string `yaml:"branch,omitempty"`
}

// The registry is the directory itself: one folder per app, each with an
// app.yaml. Nothing to keep in sync with a separate index.
func AppConfigPath(app string) string { return filepath.Join(AppDir(app), "app.yaml") }

func LoadApp(name string) (*App, error) {
	path := AppConfigPath(name)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("app %q is not registered — run `hploy add` first", name)
	}
	if err != nil {
		return nil, err
	}

	var a App
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if a.Name == "" {
		a.Name = name
	}
	if err := a.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &a, nil
}

func SaveApp(a *App) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(AppDir(a.Name), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(AppConfigPath(a.Name), b, 0o600)
}

func ListApps() ([]*App, error) {
	entries, err := os.ReadDir(filepath.Join(Dir(), "apps"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []*App
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		a, err := LoadApp(e.Name())
		if err != nil {
			// A folder without a readable app.yaml is not an app yet.
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SpecPath prefers the spec committed in the repo. The copy under the store is
// a bootstrap, so an app can be adopted without touching its repository;
// committing the spec later is an upgrade, not a requirement.
func (a *App) SpecPath() string {
	inRepo := filepath.Join(a.Path, "hploy.yaml")
	if _, err := os.Stat(inRepo); err == nil {
		return inRepo
	}
	return filepath.Join(AppDir(a.Name), "hploy.yaml")
}

var reAppName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

func (a *App) Validate() error {
	if !reAppName.MatchString(a.Name) {
		return fmt.Errorf("app %q is invalid: lowercase, digits and hyphen", a.Name)
	}
	if !filepath.IsAbs(a.Path) {
		return fmt.Errorf("path must be absolute (got %q)", a.Path)
	}
	seen := map[string]bool{}
	for i, d := range a.Domains {
		clean, err := CleanDomain(d)
		if err != nil {
			return err
		}
		if seen[clean] {
			return fmt.Errorf("domain %q is listed twice", clean)
		}
		seen[clean] = true
		a.Domains[i] = clean
	}
	return nil
}

var reDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// CleanDomain normalizes a hostname and names the mistake when it cannot.
// The wizard pastes whatever came from the browser's address bar.
func CleanDomain(d string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(d))
	switch {
	case clean == "":
		return "", fmt.Errorf("empty domain")
	case strings.Contains(clean, "://"):
		return "", fmt.Errorf("domain %q: drop the scheme, just the hostname", d)
	case strings.Contains(clean, "/"):
		return "", fmt.Errorf("domain %q: a domain has no path", d)
	case strings.Contains(clean, ":"):
		return "", fmt.Errorf("domain %q: a domain has no port — the proxy owns 443", d)
	case !reDomain.MatchString(clean):
		return "", fmt.Errorf("domain %q is not a valid hostname", d)
	}
	return clean, nil
}
