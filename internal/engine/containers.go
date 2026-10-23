package engine

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// LabelApp marks a container as ours. Using a label as the runtime source of
// truth means that if the hploy daemon dies the state is not lost -- and
// `docker ps` stays readable for a human.
const LabelApp = "hploy.app"

// LabelService names which service of the app this container is.
const LabelService = "hploy.service"

// LabelRelease records the commit the image was built from.
const LabelRelease = "hploy.release"

// LabelSpecHash is the fingerprint of the resolved service config. See
// spec.Service.Hash for why this exists.
const LabelSpecHash = "hploy.spec-hash"

// response of GET /containers/json -- only the fields we use.
type apiContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

// Docker does not return health as its own field in the listing: it is baked
// into the Status text, as "Up 2 hours (healthy)". This is what the docker CLI
// itself does -- and it avoids one inspect call per container.
var reHealth = regexp.MustCompile(`\((healthy|unhealthy|health: starting|starting)\)`)

// List returns the containers, ours first.
func (c *Client) List(ctx context.Context, all bool) ([]Container, error) {
	path := "/containers/json"
	if all {
		path += query("all", "1")
	}

	var raw []apiContainer
	if err := c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}

	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		out = append(out, Container{
			ID:        shortID(r.ID),
			Name:      firstName(r.Names),
			Image:     r.Image,
			State:     r.State,
			Status:    r.Status,
			Health:    health(r.Status),
			App:       r.Labels[LabelApp],
			CreatedAt: time.Unix(r.Created, 0),
			Ports:     ports(r),
		})
	}

	// Our containers first, then alphabetical. Whoever runs the command wants
	// to see their own stuff at the top.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Managed() != out[j].Managed() {
			return out[i].Managed()
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// Names arrive with a leading slash ("/nginx-proxy") and a container may have
// several aliases; the first one is the primary.
func firstName(names []string) string {
	if len(names) == 0 {
		return "<unnamed>"
	}
	return strings.TrimPrefix(names[0], "/")
}

func health(status string) string {
	m := reHealth.FindStringSubmatch(status)
	if m == nil {
		return ""
	}
	if strings.Contains(m[1], "starting") {
		return "starting"
	}
	return m[1]
}

func ports(r apiContainer) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range r.Ports {
		var s string
		if p.PublicPort != 0 {
			ip := p.IP
			if ip == "0.0.0.0" || ip == "::" {
				ip = "*" // make it obvious this is open to the world
			}
			s = fmt.Sprintf("%s:%d->%d/%s", ip, p.PublicPort, p.PrivatePort, p.Type)
		} else {
			s = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
