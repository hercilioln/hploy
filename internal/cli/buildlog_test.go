package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/hercilioln/hploy/internal/ui"
)

func feed(b *buildLog, chunks ...string) {
	for _, c := range chunks {
		fmt.Fprint(b, c)
	}
}

// The default is a quiet screen: the build's own output never scrolls past.
func TestQuietBuildPrintsNothing(t *testing.T) {
	var screen bytes.Buffer
	b := newBuildLog(ui.NewConsole(&screen), false)

	feed(b, "Step 1/3 : FROM php:8.3-fpm\n", " ---> 3b504855cfd1\n",
		"Step 2/3 : RUN pecl install imagick\n", "configuration option \"php_ini\" is not set\n")

	if screen.Len() != 0 {
		t.Errorf("nothing should have reached the screen:\n%s", screen.String())
	}
}

func TestVerboseBuildPrintsEverything(t *testing.T) {
	var screen bytes.Buffer
	b := newBuildLog(ui.NewConsole(&screen), true)

	feed(b, "Step 1/3 : FROM php:8.3-fpm\n", " ---> 3b504855cfd1\n")

	out := screen.String()
	for _, want := range []string{"Step 1/3", "3b504855cfd1"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
}

// The docker stream does not arrive line by line, so a line split across two
// writes has to be reassembled before it is parsed or stored.
func TestPartialLinesAreReassembled(t *testing.T) {
	b := newBuildLog(ui.NewConsole(&bytes.Buffer{}), false)

	feed(b, "Step 1/14 : ", "RUN composer ", "install\n")

	if len(b.tail) != 1 {
		t.Fatalf("tail = %v, want one assembled line", b.tail)
	}
	if b.tail[0] != "Step 1/14 : RUN composer install" {
		t.Errorf("tail[0] = %q", b.tail[0])
	}
	if m := reStep.FindStringSubmatch(b.tail[0]); m == nil {
		t.Error("the reassembled line should still parse as a step")
	}
}

// A failing build is unreadable without its output, so the tail is kept even
// though nothing is printed.
func TestTailKeepsTheLastLines(t *testing.T) {
	b := newBuildLog(ui.NewConsole(&bytes.Buffer{}), false)

	for i := 0; i < maxTail+25; i++ {
		feed(b, fmt.Sprintf("line %d\n", i))
	}

	if len(b.tail) != maxTail {
		t.Fatalf("tail has %d lines, want %d", len(b.tail), maxTail)
	}
	if b.tail[0] != "line 25" {
		t.Errorf("tail starts at %q, want the oldest kept line", b.tail[0])
	}
	if b.tail[len(b.tail)-1] != fmt.Sprintf("line %d", maxTail+24) {
		t.Errorf("tail ends at %q", b.tail[len(b.tail)-1])
	}
}

func TestBlankLinesAreNotKept(t *testing.T) {
	b := newBuildLog(ui.NewConsole(&bytes.Buffer{}), false)
	feed(b, "real\n", "\n", "   \n", "\r\n", "also real\n")

	if len(b.tail) != 2 {
		t.Errorf("tail = %v, want only the two real lines", b.tail)
	}
}

func TestStepPattern(t *testing.T) {
	cases := map[string]string{
		"Step 1/14 : FROM php:8.3-fpm":          "1/14",
		"Step 12/14 : RUN pecl install imagick": "12/14",
	}
	for line, want := range cases {
		m := reStep.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("%q should have matched", line)
			continue
		}
		if m[1] != want {
			t.Errorf("%q gave step %q, want %q", line, m[1], want)
		}
	}
	for _, line := range []string{" ---> Running in abc", "Successfully built abc", "Stepping out"} {
		if reStep.MatchString(line) {
			t.Errorf("%q should not look like a step", line)
		}
	}
}

// Only a failed build should explain itself. After a successful one the tail
// is dropped, so a later failure is not blamed on it.
func TestSuccessfulBuildClearsTheTail(t *testing.T) {
	b := newBuildLog(ui.NewConsole(&bytes.Buffer{}), false)

	feed(b, "Step 1/2 : FROM alpine\n", " ---> abc\n",
		"Successfully built abc123\n", "Successfully tagged demo:latest\n")
	if len(b.tail) != 0 {
		t.Errorf("tail = %v, want it cleared", b.tail)
	}

	// A second service failing keeps only its own lines.
	feed(b, "Step 1/2 : FROM node\n", "npm ERR! missing script: build\n")
	if len(b.tail) != 2 {
		t.Fatalf("tail = %v, want the second build's lines", b.tail)
	}
	if !strings.Contains(b.tail[1], "npm ERR!") {
		t.Errorf("tail = %v", b.tail)
	}
}
