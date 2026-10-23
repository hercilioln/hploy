package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Mount attaches a named volume at a path inside the container.
type Mount struct {
	Volume string
	Target string
}

// HealthCheck is Docker's own healthcheck, which runs a command inside the
// container. An HTTP probe is not expressible here, so hploy probes those
// itself after start.
type HealthCheck struct {
	Cmd      []string
	Interval time.Duration
	Timeout  time.Duration
	Retries  int
}

// ContainerSpec is what Create needs. It is neutral on purpose: the caller
// does not build Docker's JSON.
type ContainerSpec struct {
	Name     string
	Image    string
	Cmd      []string
	Env      map[string]string
	Labels   map[string]string
	Network  string
	Aliases  []string
	Mounts   []Mount
	Memory   uint64
	NanoCPUs int64
	Health   *HealthCheck
}

func (c *Client) CreateNetwork(ctx context.Context, name string, labels map[string]string) error {
	body := map[string]any{"Name": name, "Driver": "bridge", "Labels": labels}
	return c.post(ctx, "/networks/create", body)
}

func (c *Client) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	return c.post(ctx, "/volumes/create", map[string]any{"Name": name, "Labels": labels})
}

// Create makes the container but does not start it.
func (c *Client) Create(ctx context.Context, cs ContainerSpec) (string, error) {
	env := make([]string, 0, len(cs.Env))
	for k, v := range cs.Env {
		env = append(env, k+"="+v)
	}

	mounts := make([]map[string]any, 0, len(cs.Mounts))
	for _, m := range cs.Mounts {
		mounts = append(mounts, map[string]any{
			"Type": "volume", "Source": m.Volume, "Target": m.Target,
		})
	}

	hostConfig := map[string]any{
		"Memory":        cs.Memory,
		"NanoCpus":      cs.NanoCPUs,
		"RestartPolicy": map[string]any{"Name": "unless-stopped"},
		"Mounts":        mounts,
		// Capped logs: an unbounded json-file log is how a disk fills up
		// without anyone noticing.
		"LogConfig": map[string]any{
			"Type":   "json-file",
			"Config": map[string]string{"max-size": "10m", "max-file": "3"},
		},
	}

	body := map[string]any{
		"Image":      cs.Image,
		"Env":        env,
		"Labels":     cs.Labels,
		"HostConfig": hostConfig,
		"NetworkingConfig": map[string]any{
			"EndpointsConfig": map[string]any{
				cs.Network: map[string]any{"Aliases": cs.Aliases},
			},
		},
	}
	if len(cs.Cmd) > 0 {
		body["Cmd"] = cs.Cmd
	}
	if cs.Health != nil {
		body["Healthcheck"] = map[string]any{
			"Test":     append([]string{"CMD"}, cs.Health.Cmd...),
			"Interval": cs.Health.Interval.Nanoseconds(),
			"Timeout":  cs.Health.Timeout.Nanoseconds(),
			"Retries":  cs.Health.Retries,
		}
	}

	resp, err := c.send(ctx, http.MethodPost, "/containers/create"+query("name", cs.Name), body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *Client) Start(ctx context.Context, id string) error {
	return c.post(ctx, "/containers/"+id+"/start", nil)
}

// Stop asks politely, then Docker kills after the timeout.
func (c *Client) Stop(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 10
	}
	return c.post(ctx, "/containers/"+id+"/stop"+query("t", fmt.Sprint(secs)), nil)
}

// Remove deletes the container. Volumes are never touched: they hold data.
func (c *Client) Remove(ctx context.Context, id string, force bool) error {
	path := "/containers/" + id + query("force", fmt.Sprint(force), "v", "false")
	resp, err := c.send(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PullImage fetches an image, draining the progress stream.
func (c *Client) PullImage(ctx context.Context, image string) error {
	name, tag := image, "latest"
	// Split on the last colon, but only if it is not part of a registry port.
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		name, tag = image[:i], image[i+1:]
	}
	resp, err := c.send(ctx, http.MethodPost, "/images/create"+query("fromImage", name, "tag", tag), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// The stream reports progress and, at the end, any failure. Draining it is
	// also how we wait for the pull to finish.
	dec := json.NewDecoder(resp.Body)
	for {
		var line struct {
			Error string `json:"error"`
		}
		if err := dec.Decode(&line); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if line.Error != "" {
			return fmt.Errorf("pulling %s: %s", image, line.Error)
		}
	}
}

// ImageID resolves a reference to the image it currently points at. An image
// that is not local yields an empty string, not an error: absent is a normal
// answer here.
//
// This is what tells a rebuilt tag from the same tag: the spec is unchanged
// but the ID moved.
func (c *Client) ImageID(ctx context.Context, ref string) (string, error) {
	var raw struct {
		ID string `json:"Id"`
	}
	if err := c.getJSON(ctx, "/images/"+ref+"/json", &raw); err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return raw.ID, nil
}

// Inspected is the slice of inspect output that apply needs.
type Inspected struct {
	ID      string
	State   string
	Health  string
	IP      string
	Created time.Time
}

func (c *Client) Inspect(ctx context.Context, id string) (Inspected, error) {
	var raw struct {
		ID    string `json:"Id"`
		State struct {
			Status string `json:"Status"`
			Health *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
		Created         time.Time `json:"Created"`
		NetworkSettings struct {
			Networks map[string]struct {
				IPAddress string `json:"IPAddress"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := c.getJSON(ctx, "/containers/"+id+"/json", &raw); err != nil {
		return Inspected{}, err
	}

	out := Inspected{ID: raw.ID, State: raw.State.Status, Created: raw.Created}
	if raw.State.Health != nil {
		out.Health = raw.State.Health.Status
	}
	for _, n := range raw.NetworkSettings.Networks {
		if n.IPAddress != "" {
			out.IP = n.IPAddress
			break
		}
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, path string, body any) error {
	resp, err := c.send(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
