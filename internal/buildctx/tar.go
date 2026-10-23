package buildctx

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Stats describes what the context will carry. Printing it is how a forgotten
// .dockerignore becomes visible before it is baked into an image.
type Stats struct {
	Files int
	Bytes int64
}

// Measure walks the context without reading file contents.
func Measure(dir, dockerfile string) (Stats, error) {
	var st Stats
	err := walk(dir, dockerfile, func(_ string, info fs.FileInfo) error {
		st.Files++
		if info.Mode().IsRegular() {
			st.Bytes += info.Size()
		}
		return nil
	})
	return st, err
}

// Tar streams the context. Directories, regular files and symlinks are sent;
// sockets, devices and pipes are skipped, as they cannot be built from.
func Tar(dir, dockerfile string, w io.Writer) (Stats, error) {
	tw := tar.NewWriter(w)
	var st Stats

	err := walk(dir, dockerfile, func(rel string, info fs.FileInfo) error {
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(dir, rel))
			if err != nil {
				return err
			}
			link = target
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		// Always slash-separated, and relative to the context root.
		header.Name = rel
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		st.Files++
		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(filepath.Join(dir, rel))
		if err != nil {
			return err
		}
		defer f.Close()
		n, err := io.Copy(tw, f)
		if err != nil {
			return err
		}
		if n != info.Size() {
			// The tar header already promised info.Size() bytes, so a file
			// that changed mid-walk would corrupt the stream.
			return fmt.Errorf("%s changed while being read", rel)
		}
		st.Bytes += n
		return nil
	})
	if err != nil {
		return st, err
	}
	return st, tw.Close()
}

// walk visits everything the build should receive, in a stable order.
func walk(dir, dockerfile string, visit func(rel string, info fs.FileInfo) error) error {
	ig, err := LoadIgnore(dir)
	if err != nil {
		return err
	}
	// The daemon needs these whatever .dockerignore says.
	keep := map[string]bool{".dockerignore": true}
	if dockerfile != "" {
		keep[filepath.ToSlash(filepath.Clean(dockerfile))] = true
	}

	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		if !keep[rel] && ig.Excluded(rel) {
			if d.IsDir() {
				return fs.SkipDir // do not descend into an excluded tree
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir(), info.Mode().IsRegular(), info.Mode()&os.ModeSymlink != 0:
			return visit(rel, info)
		default:
			return nil // socket, device, fifo
		}
	})
}

// DefaultDockerfile is the name used when the spec does not say.
const DefaultDockerfile = "Dockerfile"

// CleanDockerfile keeps the reference inside the context.
func CleanDockerfile(name string) (string, error) {
	if name == "" {
		return DefaultDockerfile, nil
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if filepath.IsAbs(name) || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("dockerfile %q must be inside the build context", name)
	}
	return clean, nil
}
