// Package detect reads what an application already says about itself, so the
// wizard asks only what it cannot find out.
//
// Nothing here is specific to a framework: a Dockerfile, a git remote and an
// env example are the conventions every Docker project shares.
package detect

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Project struct {
	Dir string

	// Git, empty when the directory is not a clone.
	Repo   string
	Branch string

	// Dockerfile is relative to Dir, empty when there is none.
	Dockerfile string
	Ports      []int // from EXPOSE

	HasSpec    bool // an hploy.yaml is already committed here
	HasCompose bool // a docker-compose.yml, which hploy does not read

	EnvExample string            // the example file found, relative to Dir
	EnvKeys    []string          // keys it declares, in file order
	EnvSample  map[string]string // the values it suggests, often empty
	HasEnv     bool              // an .env already exists
	EnvIgnored bool              // .env is covered by .gitignore
}

// Inspect never fails on a missing convention: an absent file is an answer.
func Inspect(dir string) (*Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "inspect", Path: abs, Err: os.ErrInvalid}
	}

	p := &Project{Dir: abs}
	p.Repo, p.Branch = gitInfo(abs)

	for _, name := range []string{"Dockerfile", "dockerfile", "docker/Dockerfile"} {
		if exists(filepath.Join(abs, name)) {
			p.Dockerfile = name
			p.Ports = exposedPorts(filepath.Join(abs, name))
			break
		}
	}

	p.HasSpec = exists(filepath.Join(abs, "hploy.yaml"))
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		if exists(filepath.Join(abs, name)) {
			p.HasCompose = true
			break
		}
	}

	for _, name := range []string{".env.example", ".env.sample", ".env.dist", "env.example"} {
		full := filepath.Join(abs, name)
		if !exists(full) {
			continue
		}
		p.EnvExample = name
		p.EnvKeys, p.EnvSample = envDecl(full)
		break
	}

	p.HasEnv = exists(filepath.Join(abs, ".env"))
	p.EnvIgnored = gitIgnores(abs, ".env")
	return p, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func gitInfo(dir string) (repo, branch string) {
	repo = strings.TrimSpace(gitOutput(dir, "remote", "get-url", "origin"))

	branch = strings.TrimSpace(gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch == "" {
		// A branch with no commit yet has no HEAD to resolve, but it does
		// have a name.
		branch = strings.TrimSpace(gitOutput(dir, "symbolic-ref", "--short", "HEAD"))
	}
	if branch == "HEAD" {
		branch = "" // detached, so there is no branch to track
	}
	return repo, branch
}

func gitOutput(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// gitIgnores answers through git itself, so every .gitignore rule counts,
// including a global one.
func gitIgnores(dir, path string) bool {
	cmd := exec.Command("git", "-C", dir, "check-ignore", "-q", path)
	return cmd.Run() == nil
}

var reExpose = regexp.MustCompile(`(?mi)^\s*EXPOSE\s+(.+)$`)

// exposedPorts reads the EXPOSE lines. They are a hint, not a contract: the
// wizard offers them as a default and the answer is still the operator's.
func exposedPorts(dockerfile string) []int {
	b, err := os.ReadFile(dockerfile)
	if err != nil {
		return nil
	}

	seen := map[int]bool{}
	for _, m := range reExpose.FindAllStringSubmatch(string(b), -1) {
		for _, field := range strings.Fields(m[1]) {
			// "8080/tcp" and build-arg forms like "${PORT}" both appear.
			number, _, _ := strings.Cut(field, "/")
			n, err := strconv.Atoi(number)
			if err != nil || n < 1 || n > 65535 {
				continue
			}
			seen[n] = true
		}
	}

	out := make([]int, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// envDecl reads an env example: the names in file order, and the values it
// suggests.
//
// The values matter as much as the names. An example usually ships real
// defaults -- APP_ENV=local, LOG_LEVEL=debug -- next to the blanks that have
// to be filled, and dropping them would make someone retype what the project
// already decided.
func envDecl(path string) ([]string, map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var keys []string
	values := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		if _, dup := values[key]; dup {
			continue
		}
		keys = append(keys, key)
		values[key] = unquote(strings.TrimSpace(value))
	}
	return keys, values
}

// unquote strips one layer of surrounding quotes, the way a dotenv reader
// does. Nothing else is interpreted.
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
