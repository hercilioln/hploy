package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hercilioln/hploy/internal/ui"
)

// maxTail is how much of a build log is kept to explain a failure.
const maxTail = 40

// buildLog decides what a build puts on screen.
//
// Quiet by default: the spinner shows the step being run and nothing scrolls.
// The output is still kept, because a build that fails is unreadable without
// it -- so the tail is printed exactly when it is needed.
type buildLog struct {
	console *ui.Console
	verbose bool

	partial strings.Builder
	tail    []string
}

var reStep = regexp.MustCompile(`^Step (\d+/\d+) : (.*)$`)

func newBuildLog(console *ui.Console, verbose bool) *buildLog {
	return &buildLog{console: console, verbose: verbose}
}

func (b *buildLog) Write(p []byte) (int, error) {
	text := string(p)
	for {
		line, rest, found := strings.Cut(text, "\n")
		if !found {
			b.partial.WriteString(line)
			break
		}
		full := b.partial.String() + line
		b.partial.Reset()
		b.line(full)
		text = rest
	}
	return len(p), nil
}

func (b *buildLog) line(s string) {
	s = strings.TrimRight(s, "\r")
	if strings.TrimSpace(s) == "" {
		return
	}

	// A finished build has nothing left to explain, and keeping its output
	// would make it look like the cause of a later failure. The daemon ends
	// with "Successfully built" and then "Successfully tagged".
	if strings.HasPrefix(s, "Successfully ") {
		b.tail = nil
	} else {
		b.tail = append(b.tail, s)
		if len(b.tail) > maxTail {
			b.tail = b.tail[len(b.tail)-maxTail:]
		}
	}

	if b.verbose {
		b.console.Printf("      %s", ui.Dim.Render(s))
		return
	}
	if m := reStep.FindStringSubmatch(s); m != nil {
		b.console.Detail(m[1] + "  " + m[2])
	}
}

// dump prints what was held back, if the build is what failed.
func (b *buildLog) dump() {
	if b.verbose || len(b.tail) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("  " + ui.Dim.Render(fmt.Sprintf("last %d lines of the build:", len(b.tail))))
	for _, line := range b.tail {
		fmt.Println("      " + ui.Dim.Render(line))
	}
	fmt.Println()
}
