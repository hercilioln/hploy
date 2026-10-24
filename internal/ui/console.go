package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
)

// Console puts hploy's own lines, a subprocess's output and an activity
// spinner on the same terminal without them overwriting each other.
//
// The spinner occupies the last line. Anything written erases it first and
// redraws it afterwards, so a build log scrolls normally with the spinner
// riding along at the bottom.
type Console struct {
	out io.Writer
	tty bool

	mu      sync.Mutex
	drawn   bool // the spinner is currently on screen
	midLine bool // the last write did not end in a newline
	label   string
	detail  string
	started time.Time
	frame   int
	stop    chan struct{}
	done    chan struct{}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// truncate keeps a line within a column. It counts runes, not bytes: cutting
// mid-rune would print a replacement character instead of a shorter word.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

// NewConsole writes to out. Without a terminal the spinner is skipped
// entirely, so piped output and CI logs stay clean.
func NewConsole(out io.Writer) *Console {
	tty := false
	if f, ok := out.(interface{ Fd() uintptr }); ok {
		tty = term.IsTerminal(f.Fd())
	}
	return &Console{out: out, tty: tty}
}

func (c *Console) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.erase()
	n, err := c.out.Write(p)
	if n > 0 {
		c.midLine = !strings.HasSuffix(string(p[:n]), "\n")
	}
	c.draw()
	return n, err
}

// Printf writes one of hploy's own lines.
func (c *Console) Printf(format string, args ...any) {
	fmt.Fprintf(c, format+"\n", args...)
}

// Busy shows that something slow is running and returns the function that
// ends it. A step with no output of its own -- pulling a base image, waiting
// for a healthcheck -- otherwise looks like a hang.
func (c *Console) Busy(label string) func() {
	if !c.tty {
		c.Printf("  %s", Dim.Render(label+"..."))
		return func() {}
	}

	c.mu.Lock()
	c.label = label
	c.detail = ""
	c.started = time.Now()
	c.frame = 0
	c.draw()
	c.stop = make(chan struct{})
	c.done = make(chan struct{})
	stop, done := c.stop, c.done
	c.mu.Unlock()

	go func() {
		defer close(done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				c.mu.Lock()
				c.frame++
				c.erase()
				c.draw()
				c.mu.Unlock()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
			c.mu.Lock()
			c.erase()
			c.label = ""
			c.mu.Unlock()
		})
	}
}

// Detail replaces the text shown after the spinner label, so a long build can
// say which step it is on without printing a line per step.
func (c *Console) Detail(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.detail == s {
		return
	}
	c.detail = s
	c.erase()
	c.draw()
}

// erase removes the spinner line. The caller holds the lock.
func (c *Console) erase() {
	if !c.drawn {
		return
	}
	io.WriteString(c.out, "\r\033[K")
	c.drawn = false
}

// draw paints the spinner, but only at the start of a line: half a log line
// is already on screen otherwise.
func (c *Console) draw() {
	if !c.tty || c.label == "" || c.midLine || c.drawn {
		return
	}
	elapsed := ""
	if d := time.Since(c.started); d >= 3*time.Second {
		elapsed = "  " + Dim.Render(Duration(d))
	}
	detail := ""
	if c.detail != "" {
		detail = "  " + Dim.Render(truncate(c.detail, 60))
	}
	fmt.Fprintf(c.out, "  %s %s%s%s",
		Warn.Render(spinnerFrames[c.frame%len(spinnerFrames)]),
		Ours.Render(c.label), detail, elapsed)
	c.drawn = true
}
