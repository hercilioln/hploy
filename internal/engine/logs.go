package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
)

// Logs devolve o stream de log do container, ja desmultiplexado.
//
// A pegadinha: quando o container NAO tem TTY, o Docker entrega o stream em
// quadros com cabecalho binario de 8 bytes (tipo do stream + tamanho), e nao
// texto puro. Jogar isso direto na tela imprime lixo no comeco de cada linha.
// Containers com TTY vem crus. Por isso consultamos o Tty antes.
func (c *Client) Logs(ctx context.Context, id string, opt LogOptions) (io.ReadCloser, error) {
	tty, err := c.hasTTY(ctx, id)
	if err != nil {
		return nil, err
	}

	tail := opt.Tail
	if tail == "" {
		tail = "100"
	}
	pares := []string{
		"stdout", "1",
		"stderr", "1",
		"tail", tail,
		"follow", strconv.FormatBool(opt.Follow),
	}
	if opt.Timestamps {
		pares = append(pares, "timestamps", "1")
	}

	resp, err := c.do(ctx, "/containers/"+id+"/logs"+q(pares...))
	if err != nil {
		return nil, err
	}
	if tty {
		return resp.Body, nil
	}
	return &demux{src: resp.Body}, nil
}

func (c *Client) hasTTY(ctx context.Context, id string) (bool, error) {
	var insp struct {
		Config struct {
			Tty bool `json:"Tty"`
		} `json:"Config"`
	}
	if err := c.getJSON(ctx, "/containers/"+id+"/json", &insp); err != nil {
		return false, err
	}
	return insp.Config.Tty, nil
}

// demux remove os cabecalhos de quadro do stream multiplexado.
//
// Formato de cada quadro:
//
//	byte 0    tipo do stream (0 stdin, 1 stdout, 2 stderr)
//	bytes 1-3 zeros
//	bytes 4-7 tamanho do payload, uint32 big-endian
//	resto     o payload
//
// Nao separamos stdout de stderr: para ver log, intercalado na ordem em que
// aconteceu e o que serve.
type demux struct {
	src   io.ReadCloser
	resto int // bytes do quadro atual ainda nao lidos
}

func (d *demux) Read(p []byte) (int, error) {
	if d.resto == 0 {
		var cab [8]byte
		if _, err := io.ReadFull(d.src, cab[:]); err != nil {
			return 0, err
		}
		tam := binary.BigEndian.Uint32(cab[4:8])
		if tam == 0 {
			return 0, nil // quadro vazio: o proximo Read busca o seguinte
		}
		// Guarda contra cabecalho corrompido virar alocacao absurda.
		if tam > 16<<20 {
			return 0, fmt.Errorf("quadro de log improvavel (%d bytes): stream fora de sincronia", tam)
		}
		d.resto = int(tam)
	}

	if len(p) > d.resto {
		p = p[:d.resto]
	}
	n, err := d.src.Read(p)
	d.resto -= n
	return n, err
}

func (d *demux) Close() error { return d.src.Close() }
