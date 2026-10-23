package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// DefaultSocket e onde o daemon escuta em instalacao padrao de Linux.
const DefaultSocket = "/var/run/docker.sock"

// Client fala com a API do Docker por HTTP sobre um socket Unix.
//
// Nao usamos o SDK oficial de proposito: a API e REST simples, e escrever o
// cliente a mao mantem o binario pequeno e deixa visivel o que esta sendo
// pedido. O preco e que escrevemos os structs de resposta que precisamos --
// so os campos usados, nao o formato inteiro.
type Client struct {
	hc     *http.Client
	socket string
}

var _ Engine = (*Client)(nil)

// New cria o cliente. Nao conecta ainda: a conexao acontece na primeira
// chamada, e o Ping serve para verificar.
func New(socket string) *Client {
	if socket == "" {
		socket = DefaultSocket
	}
	return &Client{
		socket: socket,
		hc: &http.Client{
			Transport: &http.Transport{
				// O host da URL e ignorado: toda requisicao vai para o socket.
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
				DisableCompression: true,
			},
			// Sem timeout global: o stream de stats e o follow de logs ficam
			// abertos indefinidamente. Quem controla o fim e o context.
		},
	}
}

// Socket devolve o caminho em uso, para mensagem de erro.
func (c *Client) Socket() string { return c.socket }

// do monta e executa a requisicao. O path ja vem com query string quando
// necessario, e sem prefixo de versao -- sem versao o daemon usa a mais nova
// que ele suporta, o que evita quebrar quando o Docker atualiza.
func (c *Client) do(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if _, statErr := os.Stat(c.socket); statErr != nil {
			return nil, fmt.Errorf("socket do Docker nao encontrado em %s: %w", c.socket, statErr)
		}
		return nil, fmt.Errorf("sem acesso ao socket %s (o seu usuario esta no grupo docker?): %w", c.socket, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var e struct{ Message string }
		if json.Unmarshal(corpo, &e) == nil && e.Message != "" {
			return nil, fmt.Errorf("docker respondeu %d: %s", resp.StatusCode, e.Message)
		}
		return nil, fmt.Errorf("docker respondeu %d", resp.StatusCode)
	}
	return resp, nil
}

// getJSON busca e decodifica uma resposta inteira.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// Ping confirma que o daemon responde e devolve a versao dele.
func (c *Client) Ping(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var v struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
	}
	if err := c.getJSON(ctx, "/version", &v); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s (API %s)", v.Version, v.APIVersion), nil
}

// q monta query string sem repetir url.Values em todo lugar.
func q(pares ...string) string {
	if len(pares)%2 != 0 {
		panic("q: numero impar de argumentos")
	}
	v := url.Values{}
	for i := 0; i < len(pares); i += 2 {
		v.Set(pares[i], pares[i+1])
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}
