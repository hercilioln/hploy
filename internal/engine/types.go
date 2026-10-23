// Package engine fala com o daemon do Docker.
//
// Os tipos daqui sao NOSSOS, nao os do Docker. O resto do programa depende
// deles, e nao do formato que a API devolve -- assim trocar a fonte (ou a
// versao da API) nao espalha mudanca pelo codigo todo.
package engine

import (
	"context"
	"io"
	"time"
)

// Container e o que o `hploy ps` mostra.
type Container struct {
	ID        string
	Name      string
	Image     string
	State     string // running, exited, created, paused...
	Status    string // "Up 2 hours (healthy)" -- texto do proprio Docker
	Health    string // healthy, unhealthy, starting, "" quando nao ha healthcheck
	App       string // label hploy.app, vazio para container que nao e nosso
	CreatedAt time.Time
	Ports     []string
}

// Managed diz se o container foi criado pelo hploy.
func (c Container) Managed() bool { return c.App != "" }

// Sample e uma leitura instantanea de consumo de um container.
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

	// HasCPU e falso no primeiro quadro do stream: sem leitura anterior nao
	// da para calcular o delta de CPU. A tela mostra "—" nesse caso.
	HasCPU bool
}

// MemPerc devolve o uso de memoria em relacao ao limite, ou 0 se nao houver
// limite definido.
func (s Sample) MemPerc() float64 {
	if s.MemLimit == 0 {
		return 0
	}
	return float64(s.MemUsage) / float64(s.MemLimit) * 100
}

// LogOptions controla o que o `hploy logs` pede ao daemon.
type LogOptions struct {
	Follow     bool
	Tail       string // numero ou "all"
	Timestamps bool
}

// Engine e a fronteira com o Docker. E uma interface para que o resto do
// programa (e os testes) nao precise de um daemon de verdade.
type Engine interface {
	Ping(ctx context.Context) (string, error)
	List(ctx context.Context, all bool) ([]Container, error)
	Stats(ctx context.Context, ids []string) (<-chan Sample, <-chan error)
	Logs(ctx context.Context, id string, opt LogOptions) (io.ReadCloser, error)
}
