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

// Stats opens one stream per container and merges samples into one channel.
//
// stream=true avoids the round-trip wait of stream=false, where the daemon
// collects two readings before answering. The first frame therefore arrives
// with HasCPU=false.
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

	// Container delta over machine delta, times cores -- the formula
	// `docker stats` uses.
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

// realMem subtracts page cache from usage, as `docker stats` does, so reading
// files does not look like memory consumption.
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
