package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
)

// Logs returns the container's log stream, already demultiplexed.
//
// The gotcha: when the container has NO TTY, Docker delivers the stream in
// frames with an 8-byte binary header (stream type + size), not plain text.
// Dumping that straight to the screen prints garbage at the start of every
// line. Containers with a TTY come through raw. Hence the Tty lookup first.
func (c *Client) Logs(ctx context.Context, id string, opt LogOptions) (io.ReadCloser, error) {
	tty, err := c.hasTTY(ctx, id)
	if err != nil {
		return nil, err
	}

	tail := opt.Tail
	if tail == "" {
		tail = "100"
	}
	pairs := []string{
		"stdout", "1",
		"stderr", "1",
		"tail", tail,
		"follow", strconv.FormatBool(opt.Follow),
	}
	if opt.Timestamps {
		pairs = append(pairs, "timestamps", "1")
	}

	resp, err := c.do(ctx, "/containers/"+id+"/logs"+query(pairs...))
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

// demux strips the frame headers from the multiplexed stream.
//
// Frame layout:
//
//	byte 0     stream type (0 stdin, 1 stdout, 2 stderr)
//	bytes 1-3  zeros
//	bytes 4-7  payload size, big-endian uint32
//	rest       the payload
//
// We do not split stdout from stderr: for reading logs, interleaved in the
// order things happened is what you want.
type demux struct {
	src       io.ReadCloser
	remaining int // bytes left in the current frame
}

func (d *demux) Read(p []byte) (int, error) {
	if d.remaining == 0 {
		var header [8]byte
		if _, err := io.ReadFull(d.src, header[:]); err != nil {
			return 0, err
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			return 0, nil // empty frame: the next Read fetches the following one
		}
		// Guard against a corrupt header turning into an absurd allocation.
		if size > 16<<20 {
			return 0, fmt.Errorf("implausible log frame (%d bytes): stream out of sync", size)
		}
		d.remaining = int(size)
	}

	if len(p) > d.remaining {
		p = p[:d.remaining]
	}
	n, err := d.src.Read(p)
	d.remaining -= n
	return n, err
}

func (d *demux) Close() error { return d.src.Close() }
