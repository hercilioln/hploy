package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// response of GET /containers/{id}/stats -- only what we use.
type apiStats struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IoServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

// Stats opens one stream per container and merges the samples into a single
// channel.
//
// We use stream=true on purpose: with stream=false the daemon collects two
// samples internally before answering, which costs ~1.2s of waiting per call.
// Streaming has it push one sample per second and we just read frames -- no
// round-trip latency.
//
// Visible consequence: the FIRST frame has no previous reading, so there is no
// CPU delta to compute. It arrives with HasCPU=false and the view shows "—".
func (c *Client) Stats(ctx context.Context, ids []string) (<-chan Sample, <-chan error) {
	samples := make(chan Sample, len(ids)*2)
	errs := make(chan error, len(ids))

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := c.streamOne(ctx, id, samples); err != nil && ctx.Err() == nil {
				errs <- err
			}
		}(id)
	}

	go func() {
		wg.Wait()
		close(samples)
		close(errs)
	}()

	return samples, errs
}

func (c *Client) streamOne(ctx context.Context, id string, out chan<- Sample) error {
	resp, err := c.do(ctx, "/containers/"+id+"/stats"+query("stream", "true"))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var s apiStats
		if err := dec.Decode(&s); err != nil {
			// End of stream, container stopped, or cancellation: not an error.
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case out <- convert(id, s):
		case <-ctx.Done():
			return nil
		}
	}
}

func convert(id string, s apiStats) Sample {
	sm := Sample{
		ID:       shortID(id),
		Name:     strings.TrimPrefix(s.Name, "/"),
		MemUsage: realMem(s),
		MemLimit: s.MemoryStats.Limit,
	}

	// CPU delta: how much the container consumed divided by how much the whole
	// machine consumed over the same interval, times the number of cores. This
	// is how `docker stats` itself arrives at the percentage.
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta >= 0 {
		cores := float64(s.CPUStats.OnlineCPUs)
		if cores == 0 {
			cores = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
		}
		if cores == 0 {
			cores = 1
		}
		sm.CPUPerc = cpuDelta / sysDelta * cores * 100
		sm.HasCPU = true
	}

	for _, n := range s.Networks {
		sm.NetRx += n.RxBytes
		sm.NetTx += n.TxBytes
	}
	for _, b := range s.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			sm.BlkRead += b.Value
		case "write":
			sm.BlkWrite += b.Value
		}
	}
	return sm
}

// realMem subtracts file cache from the memory usage.
//
// The raw `usage` field includes page cache, which makes a container look like
// it is eating hundreds of MB when all it did was read files. `docker stats`
// subtracts `inactive_file` (cgroup v2) or `total_inactive_file` (cgroup v1) --
// we do the same so the numbers line up.
func realMem(s apiStats) uint64 {
	usage := s.MemoryStats.Usage
	for _, key := range []string{"inactive_file", "total_inactive_file"} {
		if v, ok := s.MemoryStats.Stats[key]; ok {
			if v < usage {
				return usage - v
			}
			return 0
		}
	}
	return usage
}
