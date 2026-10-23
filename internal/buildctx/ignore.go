// Package buildctx turns a directory into the tar stream a docker build wants.
package buildctx

import (
	"os"
	"path"
	"regexp"
	"strings"
)

// Ignore is a parsed .dockerignore.
//
// Supported: comments, blank lines, "!" exceptions, "*", "?" and "**", and a
// directory pattern excluding everything under it. The last matching line
// wins, as Docker does it.
type Ignore struct {
	rules []rule
}

type rule struct {
	re     *regexp.Regexp
	negate bool
}

func ParseIgnore(content string) *Ignore {
	ig := &Ignore{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negate := strings.HasPrefix(line, "!")
		if negate {
			line = strings.TrimSpace(line[1:])
		}
		// Patterns are relative to the context root; the slashes are noise.
		line = strings.Trim(line, "/")
		if line == "" {
			continue
		}
		ig.rules = append(ig.rules, rule{re: compileGlob(line), negate: negate})
	}
	return ig
}

// LoadIgnore reads .dockerignore from a context directory. A missing file is
// an empty Ignore, not an error.
func LoadIgnore(dir string) (*Ignore, error) {
	b, err := os.ReadFile(path.Join(dir, ".dockerignore"))
	if os.IsNotExist(err) {
		return &Ignore{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseIgnore(string(b)), nil
}

// Excluded reports whether a slash-separated relative path is left out.
//
// Parent directories are tested too, so "storage" excludes
// "storage/app/file.png" without the file having to match on its own.
func (ig *Ignore) Excluded(p string) bool {
	p = strings.Trim(path.Clean(p), "/")
	if p == "." || p == "" {
		return false
	}

	candidates := []string{p}
	for dir := path.Dir(p); dir != "." && dir != "/"; dir = path.Dir(dir) {
		candidates = append(candidates, dir)
	}

	excluded := false
	for _, r := range ig.rules {
		for _, c := range candidates {
			if r.re.MatchString(c) {
				excluded = !r.negate
				break
			}
		}
	}
	return excluded
}

// compileGlob converts a Docker pattern to an anchored regexp. "**" crosses
// directory boundaries; "*" and "?" do not.
func compileGlob(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++
				// "**/" may also match nothing, so "**/x" matches a bare "x".
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		// A pattern that will not compile should match nothing rather than
		// everything: silently shipping an excluded file is the worse failure.
		return regexp.MustCompile(`$^`)
	}
	return re
}
