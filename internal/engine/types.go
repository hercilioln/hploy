// Package engine talks to the Docker daemon.
//
// The types here are ours, not Docker's, so the rest of the program does not
// depend on the API's shape.
package engine

import (
	"context"
	"io"
	"time"
)

// Container is what `hploy ps` shows.
type Container struct {
	ID        string
	Name      string
	Image     string
	ImageID   string // the resolved image, which a rebuilt tag changes
	State     string // running, exited, created, paused...
	Status    string // "Up 2 hours (healthy)" -- Docker's own wording
	Health    string // healthy, unhealthy, starting, "" when there is no healthcheck
	App       string // hploy.app label, empty for containers that are not ours
	Service   string // hploy.service label
	SpecHash  string // hploy.spec-hash label
	CreatedAt time.Time
	Ports     []string
}

// Managed reports whether hploy created this container.
func (c Container) Managed() bool { return c.App != "" }

// Sample is one instantaneous resource reading for a container.
type Sample struct {
	ID       string
	Name     string
	CPUPerc  float64
	MemUsage uint64
	MemLimit uint64
	NetRx    uint64
	NetTx    uint64
	BlkRead  uint64
	BlkWrite uint64

	// False on the first frame of a stream: a CPU delta needs a previous
	// reading.
	HasCPU bool
}

// MemPerc returns memory usage against the limit, or 0 when unlimited.
func (s Sample) MemPerc() float64 {
	if s.MemLimit == 0 {
		return 0
	}
	return float64(s.MemUsage) / float64(s.MemLimit) * 100
}

// LogOptions controls what `hploy logs` asks the daemon for.
type LogOptions struct {
	Follow     bool
	Tail       string // a number, or "all"
	Timestamps bool
}

// Engine is the boundary with Docker, as an interface so callers can be
// tested without a daemon.
type Engine interface {
	Ping(ctx context.Context) (string, error)
	List(ctx context.Context, all bool) ([]Container, error)
	ListNetworks(ctx context.Context) ([]Network, error)
	ListVolumes(ctx context.Context) ([]Volume, error)
	ImageID(ctx context.Context, ref string) (string, error)
	Stats(ctx context.Context, ids []string) (<-chan Sample, <-chan error)
	Logs(ctx context.Context, id string, opt LogOptions) (io.ReadCloser, error)
}
