// Package cli monta a superficie de comandos.
//
// Regra da casa: comando monta dados e chama o pacote ui para desenhar. Nao
// se escreve string colorida aqui dentro.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hercilioln/hploy/internal/engine"
	"github.com/spf13/cobra"
)

// Version e sobrescrita no build: -ldflags "-X .../cli.Version=v0.2.0"
var Version = "dev"

var socketFlag string

func Execute() {
	// Ctrl+C precisa fechar o stream de stats e o follow de logs com
	// elegancia, nao matar o processo no meio de um quadro.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := raiz().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func raiz() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hploy",
		Short: "Orquestra containers e publica aplicacoes numa VPS",
		Long: "hploy — orquestracao de containers pelo terminal.\n\n" +
			"Esta versao e SOMENTE LEITURA: observa o que ja roda na maquina e\n" +
			"nao cria, altera nem remove nada.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&socketFlag, "socket", engine.DefaultSocket,
		"caminho do socket do Docker")

	cmd.AddCommand(cmdPs(), cmdStats(), cmdLogs(), cmdVersion())
	return cmd
}

// motor devolve o cliente do Docker ja verificado. Falhar aqui, com mensagem
// clara, e melhor que falhar no meio de um comando.
func motor(ctx context.Context) (*engine.Client, error) {
	c := engine.New(socketFlag)
	if _, err := c.Ping(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Mostra a versao do hploy e do daemon do Docker",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Println("hploy", Version)
			c := engine.New(socketFlag)
			v, err := c.Ping(cmd.Context())
			if err != nil {
				fmt.Println("docker: indisponivel —", err)
				return nil
			}
			fmt.Println("docker", v)
			return nil
		},
	}
}
