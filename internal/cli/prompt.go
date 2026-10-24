package cli

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/hercilioln/hploy/internal/ui"
)

// prompter asks questions on the terminal and remembers how many lines it has
// put there, which is what makes stepping backwards possible: a wizard that
// goes back erases the answer it is about to ask again, instead of printing
// the same question twice.
//
// Every line a wizard prints has to go through this type. One printed behind
// its back and the count is wrong, which shows up as a duplicated line.
//
// Without a terminal it refuses rather than blocking: a wizard waiting on a
// closed stdin looks like a hang, and this runs over SSH.
type prompter struct {
	lines int
}

func newPrompter() (*prompter, error) {
	if !isTTY() {
		return nil, fmt.Errorf("this command asks questions and stdin is not a terminal")
	}
	return &prompter{}, nil
}

func isTTY() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

// say prints one line and counts it.
func (p *prompter) say(line string) {
	fmt.Println(line)
	p.lines++
}

func (p *prompter) blank() { p.say("") }

// section is the heading above a group of questions.
func (p *prompter) section(title string) {
	p.blank()
	p.say("  " + ui.Header.Render(title))
}

// hint is an explanation that is not an answer.
func (p *prompter) hint(text string) { p.say("  " + dim(text)) }

// erase removes the last n lines, both from the screen and from the count.
func (p *prompter) erase(n int) {
	for i := 0; i < n; i++ {
		fmt.Print("\033[1A\033[2K\r")
	}
	if n > 0 {
		p.lines -= n
	}
}

// wizard runs questions in order, with esc going back one.
//
// A step that returns ui.ErrBack is re-run after the step before it, whose
// lines are erased first so the transcript still reads top to bottom. Backing
// out of the first step returns ui.ErrBack to the caller, which is the only
// one that knows whether that means "cancel" or "up one level".
//
// A step with nothing to ask returns ui.ErrSkip and the wizard keeps going
// the way it already was. Without that, stepping back into such a step would
// find it answered instantly and bounce forward again, which looks exactly
// like esc being broken.
func (p *prompter) wizard(steps ...func() error) error {
	marks := make([]int, len(steps))
	goingBack := false

	for i := 0; i < len(steps); {
		marks[i] = p.lines
		err := steps[i]()

		switch {
		case errors.Is(err, ui.ErrSkip):
			if !goingBack {
				i++
				continue
			}
			// Carry on backwards, erasing whatever the step printed on its
			// way past.
			if i == 0 {
				p.erase(p.lines - marks[i])
				return ui.ErrBack
			}
			i--
			p.erase(p.lines - marks[i])

		case errors.Is(err, ui.ErrBack):
			goingBack = true
			// Whatever this step left on screen, including its heading.
			p.erase(p.lines - marks[i])
			if i == 0 {
				return ui.ErrBack
			}
			i--
			p.erase(p.lines - marks[i])

		case err != nil:
			return err

		default:
			goingBack = false
			i++
		}
	}
	return nil
}

// ask returns def when the answer is empty.
func (p *prompter) ask(label, def string) (string, error) {
	return p.count(ui.Input(ui.Ask{
		Label: label, Hint: hintFor(def), Default: def, Back: true,
	}))
}

// askValid re-asks until validate accepts, so a typo costs one keystroke
// instead of restarting the wizard.
func (p *prompter) askValid(label, def string, validate func(string) error) (string, error) {
	return p.count(ui.Input(ui.Ask{
		Label: label, Hint: hintFor(def), Default: def, Back: true, Validate: validate,
	}))
}

func (p *prompter) choose(label string, options []string, def int) (int, error) {
	choice, err := ui.Select(ui.Choice{
		Label: label, Options: options, Default: def, Back: true,
	})
	if err != nil {
		return 0, err
	}
	p.lines++
	return choice, nil
}

func (p *prompter) yesNo(label string, def bool) (bool, error) {
	cursor := 1
	if def {
		cursor = 0
	}
	choice, err := p.choose(label, []string{"yes", "no"}, cursor)
	if err != nil {
		return false, err
	}
	return choice == 0, nil
}

// count charges one line for an answered question. An abandoned one renders
// nothing, so it costs nothing.
func (p *prompter) count(answer string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	p.lines++
	return answer, nil
}

func hintFor(def string) string {
	if def == "" {
		return ""
	}
	return "default: " + def
}

// generators are the shapes ctrl+g offers in the env panel. Nothing picks one
// automatically: an application decides what it accepts, and 32 random bytes
// in the wrong shape is not a secret, it is a boot failure.
//
// Laravel is the case that proves it. It reads APP_KEY as raw bytes unless
// the value starts with "base64:", and then checks the length against the
// cipher -- so a url-safe secret in that field is 43 bytes where AES-256
// wants 32, and the app throws "Unsupported cipher or incorrect key length".
var generators = []ui.Generator{
	{
		Label: "32 random bytes, url-safe — passwords, tokens, webhook secrets",
		New:   randomSecret,
		Match: func(v string) bool {
			return len(v) == 43 && !strings.ContainsAny(v, "+/=")
		},
	},
	{
		Label: "base64: + 32 random bytes — Laravel's APP_KEY",
		New:   func() string { return "base64:" + base64.StdEncoding.EncodeToString(randomBytes(32)) },
		Match: func(v string) bool { return strings.HasPrefix(v, "base64:") },
	},
	{
		Label: "64 hex characters — 32 bytes, for what insists on hex",
		New:   func() string { return hex.EncodeToString(randomBytes(32)) },
		Match: func(v string) bool {
			if len(v) != 64 {
				return false
			}
			_, err := hex.DecodeString(v)
			return err == nil
		},
	},
}

// randomSecret returns 32 bytes of entropy, URL-safe so it survives being
// pasted into a shell or an env file unquoted.
func randomSecret() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(32))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on Linux; refusing beats a weak secret.
		panic("no entropy available: " + err.Error())
	}
	return b
}
