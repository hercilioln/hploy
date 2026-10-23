// Package host reads machine metrics straight from /proc.
//
// No external library on purpose: it is four text files and one syscall, and
// reading them by hand is exactly what teaches you where the numbers from
// `free` and `uptime` come from.
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

// MemUsed is what `free` calls "used": total minus what is available.
func (i Info) MemUsed() uint64 { return i.MemTotal - i.MemAvail }

func (i Info) MemPerc() float64  { return perc(i.MemUsed(), i.MemTotal) }
func (i Info) SwapPerc() float64 { return perc(i.SwapUsed, i.SwapTotal) }
func (i Info) DiskPerc() float64 { return perc(i.DiskTotal-i.DiskFree, i.DiskTotal) }

func perc(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// Read collects everything. A failure in one file does not stop the others:
// the view shows zero for that field instead of showing nothing at all.
func Read() (Info, error) {
	i := Info{CPUs: runtime.NumCPU()}
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	keep(i.readMeminfo())
	keep(i.readLoadavg())
	keep(i.readUptime())
	keep(i.readDisk("/"))
	return i, firstErr
}

func (i *Info) readMeminfo() error {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return err
	}
	defer f.Close()

	var swapFree uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		// Values come in kB.
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		v *= 1024
		switch strings.TrimSuffix(fields[0], ":") {
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

func (i *Info) readLoadavg() error {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return fmt.Errorf("unexpected format in /proc/loadavg")
	}
	i.Load1, _ = strconv.ParseFloat(fields[0], 64)
	i.Load5, _ = strconv.ParseFloat(fields[1], 64)
	i.Load15, _ = strconv.ParseFloat(fields[2], 64)
	return nil
}

func (i *Info) readUptime() error {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return fmt.Errorf("unexpected format in /proc/uptime")
	}
	s, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return err
	}
	i.Uptime = time.Duration(s) * time.Second
	return nil
}

func (i *Info) readDisk(mount string) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(mount, &st); err != nil {
		return err
	}
	i.DiskTotal = st.Blocks * uint64(st.Bsize)
	// Bavail is free space for an ordinary user; Bfree includes the blocks
	// reserved for root, which is why `df` shows the smaller number.
	i.DiskFree = st.Bavail * uint64(st.Bsize)
	return nil
}
