package engine

import (
	"context"
	"net/http"
	"sort"
)

// Network is a Docker network, reduced to what plan needs.
type Network struct {
	Name string
	App  string
}

// Volume is a Docker volume, reduced to what plan needs.
type Volume struct {
	Name string
	App  string
}

func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	var raw []struct {
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
	}
	if err := c.getJSON(ctx, "/networks", &raw); err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(raw))
	for _, r := range raw {
		out = append(out, Network{Name: r.Name, App: r.Labels[LabelApp]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Client) ListVolumes(ctx context.Context) ([]Volume, error) {
	var raw struct {
		Volumes []struct {
			Name   string            `json:"Name"`
			Labels map[string]string `json:"Labels"`
		} `json:"Volumes"`
	}
	if err := c.getJSON(ctx, "/volumes", &raw); err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(raw.Volumes))
	for _, r := range raw.Volumes {
		out = append(out, Volume{Name: r.Name, App: r.Labels[LabelApp]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// RemoveNetwork deletes a network. It fails while a container is still
// attached, which is the right order of operations.
func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	resp, err := c.send(ctx, http.MethodDelete, "/networks/"+name, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// RemoveVolume deletes a volume and the data in it. Only ever called when
// someone asked for that by name.
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	resp, err := c.send(ctx, http.MethodDelete, "/volumes/"+name, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
