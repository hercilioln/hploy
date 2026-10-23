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
	var observar bool
	var intervalo time.Duration

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Mostra consumo da maquina e dos containers",
		Long: "Sem flags, imprime uma amostra em texto (serve para script).\n" +
			"Com -w, abre a tela ao vivo.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := motor(cmd.Context())
			if err != nil {
				return err
			}
			if observar {
				return rodarTUI(cmd.Context(), c, intervalo)
			}
			return umaAmostra(cmd.Context(), c)
		},
	}
	cmd.Flags().BoolVarP(&observar, "watch", "w", false, "tela ao vivo")
	// 2s e nao 1s de proposito: cada refresh custa ~31 ms de CPU do daemon
	// com 8 containers, e de nada serve gastar isso duas vezes mais rapido.
	cmd.Flags().DurationVarP(&intervalo, "interval", "i", 2*time.Second,
		"intervalo de atualizacao no modo -w")
	return cmd
}

// coletar junta uma leitura do host e uma amostra de cada container em
// execucao. Espera o SEGUNDO quadro de cada stream, porque o primeiro nao tem
// leitura anterior e viria sem CPU.
func coletar(ctx context.Context, c *engine.Client) (host.Info, []engine.Sample, error) {
	hi, _ := host.Read() // erro parcial nao impede mostrar o resto

	lista, err := c.List(ctx, false)
	if err != nil {
		return hi, nil, err
	}
	var ids []string
	for _, ct := range lista {
		if ct.State == "running" {
			ids = append(ids, ct.ID)
		}
	}
	if len(ids) == 0 {
		return hi, nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	amostras, erros := c.Stats(ctx, ids)
	melhor := map[string]engine.Sample{}
	for len(melhor) < len(ids) {
		select {
		case a, ok := <-amostras:
			if !ok {
				goto fim
			}
			// Guarda a amostra; sobrescreve enquanto nao tiver CPU calculada.
			if anterior, existe := melhor[a.ID]; !existe || (!anterior.HasCPU && a.HasCPU) {
				melhor[a.ID] = a
			}
			// Para quando todos ja tem CPU.
			completos := 0
			for _, a := range melhor {
				if a.HasCPU {
					completos++
				}
			}
			if completos == len(ids) {
				goto fim
			}
		case err := <-erros:
			if err != nil {
				return hi, nil, err
			}
		case <-ctx.Done():
			goto fim
		}
	}
fim:
	out := make([]engine.Sample, 0, len(melhor))
	for _, a := range melhor {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPUPerc > out[j].CPUPerc })
	return hi, out, nil
}

func umaAmostra(ctx context.Context, c *engine.Client) error {
	hi, amostras, err := coletar(ctx, c)
	if err != nil {
		return err
	}
	fmt.Print(painelHost(hi))
	if len(amostras) == 0 {
		fmt.Println(ui.Aviso("\nnenhum container em execucao"))
		return nil
	}
	fmt.Println()
	fmt.Print(tabelaContainers(amostras, hi.MemTotal))
	return nil
}

func painelHost(hi host.Info) string {
	t := ui.NewTable("recurso", "uso", "", "detalhe")
	t.AlignRight(1)

	t.Row("memoria",
		fmt.Sprintf("%.0f%%", hi.MemPerc()),
		ui.Barra(hi.MemPerc(), 20),
		fmt.Sprintf("%s de %s", ui.Bytes(hi.MemUsed()), ui.Bytes(hi.MemTotal)))

	detalheSwap := fmt.Sprintf("%s de %s", ui.Bytes(hi.SwapUsed), ui.Bytes(hi.SwapTotal))
	if hi.SwapUsed > 0 {
		// Swap em uso numa VPS pequena e sinal de pressao de memoria.
		detalheSwap = ui.Atencao.Render(detalheSwap)
	}
	t.Row("swap",
		fmt.Sprintf("%.0f%%", hi.SwapPerc()),
		ui.Barra(hi.SwapPerc(), 20),
		detalheSwap)

	t.Row("disco /",
		fmt.Sprintf("%.0f%%", hi.DiskPerc()),
		ui.Barra(hi.DiskPerc(), 20),
		fmt.Sprintf("%s livres de %s", ui.Bytes(hi.DiskFree), ui.Bytes(hi.DiskTotal)))

	cargaPerc := hi.Load1 / float64(hi.CPUs) * 100
	t.Row("carga 1m",
		fmt.Sprintf("%.2f", hi.Load1),
		ui.Barra(cargaPerc, 20),
		fmt.Sprintf("%d vCPU · 5m %.2f · 15m %.2f", hi.CPUs, hi.Load5, hi.Load15))

	cabecalho := ui.Titulo.Render("maquina") + "  " +
		ui.Apagado.Render("no ar ha "+ui.Duracao(hi.Uptime))
	return cabecalho + "\n" + t.String()
}

func tabelaContainers(amostras []engine.Sample, memHost uint64) string {
	t := ui.NewTable("container", "cpu", "", "memoria", "", "rede", "disco")
	t.AlignRight(1, 3, 5, 6)
	for _, a := range amostras {
		t.Row(
			a.Name,
			ui.Perc(a.CPUPerc, a.HasCPU),
			ui.Barra(a.CPUPerc, 12),
			memoria(a, memHost),
			ui.Barra(a.MemPerc(), 12),
			fmt.Sprintf("%s↓ %s↑", ui.Bytes(a.NetRx), ui.Bytes(a.NetTx)),
			fmt.Sprintf("%s↓ %s↑", ui.Bytes(a.BlkRead), ui.Bytes(a.BlkWrite)),
		)
	}
	return ui.Titulo.Render("containers") + "\n" + t.String()
}

// memoria mostra uso e limite juntos.
//
// Sem o limite visivel, um container sem restricao aparece com a barra vazia
// (61 MB de 8 GB sao 0,7%) e parece defeito. Com "61MB/host" fica claro que a
// referencia e a maquina inteira, nao um limite do container.
func memoria(a engine.Sample, memHost uint64) string {
	uso := ui.Bytes(a.MemUsage)
	// O Docker devolve a memoria do host como limite quando nao ha restricao.
	if memHost > 0 && a.MemLimit >= uint64(float64(memHost)*0.95) {
		return uso + ui.Apagado.Render("/host")
	}
	return uso + ui.Apagado.Render("/"+ui.Bytes(a.MemLimit))
}
