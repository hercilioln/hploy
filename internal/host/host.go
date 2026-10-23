// Package host le as metricas da maquina direto do /proc.
//
// Sem biblioteca externa de proposito: sao quatro arquivos de texto e uma
// syscall, e ler isso a mao e justamente o que ensina onde os numeros do
// `free` e do `uptime` nascem.
package host

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Info struct {
	CPUs      int
	Load1     float64
	Load5     float64
	Load15    float64
	MemTotal  uint64
	MemAvail  uint64
	SwapTotal uint64
	SwapUsed  uint64
	DiskTotal uint64
	DiskFree  uint64
	Uptime    time.Duration
}

// MemUsed e o que o `free` chama de "used": total menos o que esta disponivel.
func (i Info) MemUsed() uint64 { return i.MemTotal - i.MemAvail }

func (i Info) MemPerc() float64  { return perc(i.MemUsed(), i.MemTotal) }
func (i Info) SwapPerc() float64 { return perc(i.SwapUsed, i.SwapTotal) }
func (i Info) DiskPerc() float64 {
	return perc(i.DiskTotal-i.DiskFree, i.DiskTotal)
}

func perc(parte, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(parte) / float64(total) * 100
}

// Read coleta tudo. Falha em um arquivo nao impede os outros: a tela mostra
// zero no campo em vez de nao mostrar nada.
func Read() (Info, error) {
	i := Info{CPUs: runtime.NumCPU()}
	var primeiroErro error
	guarda := func(err error) {
		if err != nil && primeiroErro == nil {
			primeiroErro = err
		}
	}

	guarda(i.lerMeminfo())
	guarda(i.lerLoadavg())
	guarda(i.lerUptime())
	guarda(i.lerDisco("/"))
	return i, primeiroErro
}

func (i *Info) lerMeminfo() error {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return err
	}
	defer f.Close()

	var swapFree uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		campos := strings.Fields(sc.Text())
		if len(campos) < 2 {
			continue
		}
		// Os valores vem em kB.
		v, err := strconv.ParseUint(campos[1], 10, 64)
		if err != nil {
			continue
		}
		v *= 1024
		switch strings.TrimSuffix(campos[0], ":") {
		case "MemTotal":
			i.MemTotal = v
		case "MemAvailable":
			i.MemAvail = v
		case "SwapTotal":
			i.SwapTotal = v
		case "SwapFree":
			swapFree = v
		}
	}
	i.SwapUsed = i.SwapTotal - swapFree
	return sc.Err()
}

func (i *Info) lerLoadavg() error {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return err
	}
	campos := strings.Fields(string(b))
	if len(campos) < 3 {
		return fmt.Errorf("/proc/loadavg em formato inesperado")
	}
	i.Load1, _ = strconv.ParseFloat(campos[0], 64)
	i.Load5, _ = strconv.ParseFloat(campos[1], 64)
	i.Load15, _ = strconv.ParseFloat(campos[2], 64)
	return nil
}

func (i *Info) lerUptime() error {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return err
	}
	campos := strings.Fields(string(b))
	if len(campos) < 1 {
		return fmt.Errorf("/proc/uptime em formato inesperado")
	}
	s, err := strconv.ParseFloat(campos[0], 64)
	if err != nil {
		return err
	}
	i.Uptime = time.Duration(s) * time.Second
	return nil
}

func (i *Info) lerDisco(ponto string) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(ponto, &st); err != nil {
		return err
	}
	i.DiskTotal = st.Blocks * uint64(st.Bsize)
	// Bavail e o espaco livre para usuario comum; Bfree inclui o reservado
	// para o root, e e por isso que o `df` mostra numero menor.
	i.DiskFree = st.Bavail * uint64(st.Bsize)
	return nil
}
