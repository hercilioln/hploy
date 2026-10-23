package cli

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/hercilioln/hploy/internal/host"
	"github.com/hercilioln/hploy/internal/ui"
	"github.com/spf13/cobra"
)

func cmdStats() *cobra.Command {
	var watch bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show machine and container resource usage",
		Long: "With no flags it prints one sample as plain text (good for scripts).\n" +
			"With -w it opens the live view.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := dockerEngine(cmd.Context())
			if err != nil {
				return err
			}
			if watch {
				return runTUI(cmd.Context(), c, interval)
			}
			return oneSample(cmd.Context(), c)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "live view")
	// Each refresh costs daemon CPU, so the default is deliberately slow.
	cmd.Flags().DurationVarP(&interval, "interval", "i", 2*time.Second,
		"refresh interval in -w mode")
	return cmd
}

// collect gathers one host reading plus one sample per running container,
// waiting for the second frame of each stream so CPU is populated.
func collect(ctx context.Context, c *engine.Client) (host.Info, []engine.Sample, error) {
	hi, _ := host.Read() // a partial reading still beats showing nothing

	list, err := c.List(ctx, false)
	if err != nil {
		return hi, nil, err
	}
	var ids []string
	for _, ct := range list {
		if ct.State == "running" {
			ids = append(ids, ct.ID)
		}
	}
	if len(ids) == 0 {
		return hi, nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	samples, errs := c.Stats(ctx, ids)
	best := map[string]engine.Sample{}
	for len(best) < len(ids) {
		select {
		case s, ok := <-samples:
			if !ok {
				goto done
			}
			// Keep the sample; overwrite while it still lacks a CPU figure.
			if prev, exists := best[s.ID]; !exists || (!prev.HasCPU && s.HasCPU) {
				best[s.ID] = s
			}
			// Stop once every container has a CPU figure.
			complete := 0
			for _, s := range best {
				if s.HasCPU {
					complete++
				}
			}
			if complete == len(ids) {
				goto done
			}
		case err := <-errs:
			if err != nil {
				return hi, nil, err
			}
		case <-ctx.Done():
			goto done
		}
	}
done:
	out := make([]engine.Sample, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPUPerc > out[j].CPUPerc })
	return hi, out, nil
}

func oneSample(ctx context.Context, c *engine.Client) error {
	hi, samples, err := collect(ctx, c)
	if err != nil {
		return err
	}
	fmt.Print(hostPanel(hi))
	if len(samples) == 0 {
		fmt.Println()
		fmt.Println(ui.Note("no containers running"))
		return nil
	}
	fmt.Println()
	fmt.Print(containerTable(samples, hi.MemTotal))
	return nil
}

func hostPanel(hi host.Info) string {
	t := ui.NewTable("resource", "usage", "", "detail")
	t.AlignRight(1)

	t.Row("memory",
		fmt.Sprintf("%.0f%%", hi.MemPerc()),
		ui.Bar(hi.MemPerc(), 20),
		fmt.Sprintf("%s of %s", ui.Bytes(hi.MemUsed()), ui.Bytes(hi.MemTotal)))

	swapDetail := fmt.Sprintf("%s of %s", ui.Bytes(hi.SwapUsed), ui.Bytes(hi.SwapTotal))
	if hi.SwapUsed > 0 {
		// Any swap in use is worth flagging.
		swapDetail = ui.Warn.Render(swapDetail)
	}
	t.Row("swap",
		fmt.Sprintf("%.0f%%", hi.SwapPerc()),
		ui.Bar(hi.SwapPerc(), 20),
		swapDetail)

	t.Row("disk /",
		fmt.Sprintf("%.0f%%", hi.DiskPerc()),
		ui.Bar(hi.DiskPerc(), 20),
		fmt.Sprintf("%s free of %s", ui.Bytes(hi.DiskFree), ui.Bytes(hi.DiskTotal)))

	loadPerc := hi.Load1 / float64(hi.CPUs) * 100
	t.Row("load 1m",
		fmt.Sprintf("%.2f", hi.Load1),
		ui.Bar(loadPerc, 20),
		fmt.Sprintf("%d vCPU · 5m %.2f · 15m %.2f", hi.CPUs, hi.Load5, hi.Load15))

	header := ui.Title.Render("machine") + "  " +
		ui.Dim.Render("up "+ui.Duration(hi.Uptime))
	return header + "\n" + t.String()
}

func containerTable(samples []engine.Sample, hostMem uint64) string {
	t := ui.NewTable("container", "cpu", "", "memory", "", "network", "disk")
	t.AlignRight(1, 3, 5, 6)
	for _, s := range samples {
		t.Row(
			s.Name,
			ui.Perc(s.CPUPerc, s.HasCPU),
			ui.Bar(s.CPUPerc, 12),
			memoryCell(s, hostMem),
			ui.Bar(s.MemPerc(), 12),
			fmt.Sprintf("%s↓ %s↑", ui.Bytes(s.NetRx), ui.Bytes(s.NetTx)),
			fmt.Sprintf("%s↓ %s↑", ui.Bytes(s.BlkRead), ui.Bytes(s.BlkWrite)),
		)
	}
	return ui.Title.Render("containers") + "\n" + t.String()
}

// memoryCell shows usage over limit, or "/host" when the container has none --
// otherwise the bar sits at near-zero and looks broken.
func memoryCell(s engine.Sample, hostMem uint64) string {
	usage := ui.Bytes(s.MemUsage)
	// Docker reports the host memory as the limit when none is set.
	if hostMem > 0 && s.MemLimit >= uint64(float64(hostMem)*0.95) {
		return usage + ui.Dim.Render("/host")
	}
	return usage + ui.Dim.Render("/"+ui.Bytes(s.MemLimit))
}
