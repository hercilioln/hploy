package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// resposta de GET /containers/{id}/stats -- so o que usamos.
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

// Stats abre um stream por container e devolve as amostras num canal unico.
//
// Usamos stream=true de proposito: com stream=false o daemon coleta duas
// amostras internamente e so entao responde, o que custa ~1,2s de espera por
// chamada. Em streaming ele empurra uma amostra por segundo e nos so lemos os
// quadros -- sem latencia de ida e volta.
//
// Consequencia visivel: o PRIMEIRO quadro nao tem leitura anterior, entao nao
// da para calcular delta de CPU. Ele vem com HasCPU=false e a tela mostra "—".
func (c *Client) Stats(ctx context.Context, ids []string) (<-chan Sample, <-chan error) {
	amostras := make(chan Sample, len(ids)*2)
	erros := make(chan error, len(ids))

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := c.streamOne(ctx, id, amostras); err != nil && ctx.Err() == nil {
				erros <- err
			}
		}(id)
	}

	go func() {
		wg.Wait()
		close(amostras)
		close(erros)
	}()

	return amostras, erros
}

func (c *Client) streamOne(ctx context.Context, id string, out chan<- Sample) error {
	resp, err := c.do(ctx, "/containers/"+id+"/stats"+q("stream", "true"))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var s apiStats
		if err := dec.Decode(&s); err != nil {
			// Fim do stream, container parou, ou cancelamento: nao e erro.
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case out <- converte(id, s):
		case <-ctx.Done():
			return nil
		}
	}
}

func converte(id string, s apiStats) Sample {
	am := Sample{
		ID:       curto(id),
		Name:     strings.TrimPrefix(s.Name, "/"),
		MemUsage: memReal(s),
		MemLimit: s.MemoryStats.Limit,
	}

	// Delta de CPU: quanto o container consumiu dividido por quanto a maquina
	// inteira consumiu no mesmo intervalo, multiplicado pelo numero de nucleos.
	// E assim que o proprio `docker stats` chega no percentual.
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta >= 0 {
		nucleos := float64(s.CPUStats.OnlineCPUs)
		if nucleos == 0 {
			nucleos = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
		}
		if nucleos == 0 {
			nucleos = 1
		}
		am.CPUPerc = cpuDelta / sysDelta * nucleos * 100
		am.HasCPU = true
	}

	for _, n := range s.Networks {
		am.NetRx += n.RxBytes
		am.NetTx += n.TxBytes
	}
	for _, b := range s.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			am.BlkRead += b.Value
		case "write":
			am.BlkWrite += b.Value
		}
	}
	return am
}

// memReal desconta o cache de arquivo do uso de memoria.
//
// O campo `usage` cru inclui page cache, o que faz um container parecer estar
// consumindo centenas de MB quando na verdade so leu arquivos. O `docker
// stats` subtrai `inactive_file` (cgroup v2) ou `total_inactive_file`
// (cgroup v1) -- fazemos o mesmo para os numeros baterem.
func memReal(s apiStats) uint64 {
	uso := s.MemoryStats.Usage
	for _, chave := range []string{"inactive_file", "total_inactive_file"} {
		if v, ok := s.MemoryStats.Stats[chave]; ok {
			if v < uso {
				return uso - v
			}
			return 0
		}
	}
	return uso
}
