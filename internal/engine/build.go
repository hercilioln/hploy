package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hercilioln/hploy/internal/buildctx"
)

// BuildOptions is one `docker build`.
type BuildOptions struct {
	Tag        string
	Dockerfile string // relative to the context
	Target     string // multi-stage stage to stop at
	Args       map[string]string
	NoCache    bool
	Pull       bool // refresh the base image
	Labels     map[string]string
}

// Build sends the directory as a tar and streams the daemon's output to out.
// It returns the ID of the image produced.
//
// The context is piped rather than buffered: a build context is often hundreds
// of megabytes and none of it needs to be in memory at once.
func (c *Client) Build(ctx context.Context, dir string, opt BuildOptions, out io.Writer) (string, error) {
	dockerfile, err := buildctx.CleanDockerfile(opt.Dockerfile)
	if err != nil {
		return "", err
	}

	pairs := []string{
		"t", opt.Tag,
		"dockerfile", dockerfile,
		"rm", "1",
		"forcerm", "1",
		// The classic builder; BuildKit would need a session over the socket.
		"version", "1",
	}
	if opt.Target != "" {
		pairs = append(pairs, "target", opt.Target)
	}
	if opt.NoCache {
		pairs = append(pairs, "nocache", "1")
	}
	if opt.Pull {
		pairs = append(pairs, "pull", "1")
	}
	if len(opt.Args) > 0 {
		encoded, err := json.Marshal(opt.Args)
		if err != nil {
			return "", err
		}
		pairs = append(pairs, "buildargs", string(encoded))
	}
	if len(opt.Labels) > 0 {
		encoded, err := json.Marshal(opt.Labels)
		if err != nil {
			return "", err
		}
		pairs = append(pairs, "labels", string(encoded))
	}

	pr, pw := io.Pipe()
	go func() {
		_, err := buildctx.Tar(dir, dockerfile, pw)
		// Closing with the error makes the daemon's read fail rather than
		// letting it build from a truncated context.
		pw.CloseWithError(err)
	}()
	defer pr.Close()

	resp, err := c.sendStream(ctx, "/build"+query(pairs...), pr, "application/x-tar")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return readBuildStream(resp.Body, out)
}

// readBuildStream drains the daemon's JSON lines. A build failure arrives in
// the stream with a 200 status, so the body is the only place it shows up.
func readBuildStream(body io.Reader, out io.Writer) (string, error) {
	dec := json.NewDecoder(body)
	imageID := ""

	for {
		var line struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
			Aux    struct {
				ID string `json:"ID"`
			} `json:"aux"`
		}
		if err := dec.Decode(&line); err == io.EOF {
			break
		} else if err != nil {
			return imageID, err
		}

		if line.Error != "" {
			return imageID, fmt.Errorf("build failed: %s", strings.TrimSpace(line.Error))
		}
		if line.Aux.ID != "" {
			imageID = line.Aux.ID
		}
		if line.Stream != "" && out != nil {
			io.WriteString(out, line.Stream)
		}
	}
	return imageID, nil
}
