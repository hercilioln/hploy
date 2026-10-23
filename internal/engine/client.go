package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// DefaultSocket is where the daemon listens on a stock Linux install.
const DefaultSocket = "/var/run/docker.sock"

// Client talks to the Docker API over HTTP on a Unix socket.
//
// We deliberately avoid the official SDK: the API is plain REST, and writing
// the client by hand keeps the binary small and makes it obvious what is being
// requested. The price is that we declare the response structs ourselves --
// only the fields we use, not the whole payload.
type Client struct {
	hc     *http.Client
	socket string
}

var _ Engine = (*Client)(nil)

// New builds the client. It does not connect yet: the connection happens on
// the first call, and Ping is there to verify it.
func New(socket string) *Client {
	if socket == "" {
		socket = DefaultSocket
	}
	return &Client{
		socket: socket,
		hc: &http.Client{
			Transport: &http.Transport{
				// The URL host is ignored: every request goes to the socket.
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
				DisableCompression: true,
			},
			// No global timeout on purpose: the stats stream and log follow
			// stay open indefinitely. The context decides when they end.
		},
	}
}

// Socket returns the path in use, for error messages.
func (c *Client) Socket() string { return c.socket }

// do builds and runs the request. The path already carries its query string,
// and no API version prefix -- without a version the daemon uses the newest it
// supports, which avoids breaking when Docker is upgraded.
func (c *Client) do(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if _, statErr := os.Stat(c.socket); statErr != nil {
			return nil, fmt.Errorf("docker socket not found at %s: %w", c.socket, statErr)
		}
		return nil, fmt.Errorf("cannot reach socket %s (is your user in the docker group?): %w", c.socket, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var e struct{ Message string }
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			return nil, fmt.Errorf("docker returned %d: %s", resp.StatusCode, e.Message)
		}
		return nil, fmt.Errorf("docker returned %d", resp.StatusCode)
	}
	return resp, nil
}

// getJSON fetches and decodes a whole response.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// Ping confirms the daemon answers and returns its version.
func (c *Client) Ping(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var v struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
	}
	if err := c.getJSON(ctx, "/version", &v); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s (API %s)", v.Version, v.APIVersion), nil
}

// query builds a query string so url.Values does not show up everywhere.
func query(pairs ...string) string {
	if len(pairs)%2 != 0 {
		panic("query: odd number of arguments")
	}
	v := url.Values{}
	for i := 0; i < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}
