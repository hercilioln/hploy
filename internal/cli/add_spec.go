package cli

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hercilioln/hploy/internal/detect"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/ui"
	"gopkg.in/yaml.v3"
)

// scalarNode wraps one answer so the spec's own unmarshaller validates it,
// instead of this file reimplementing "512m".
func scalarNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// generateSpec asks the handful of things a project cannot state about itself
// and returns the YAML. It is written to the store, never into the clone.
//
// The questions are a wizard, not an interrogation: esc goes back one, and
// the answer already given is the default, so a wrong turn costs one key.
func generateSpec(p *prompter, app string, project *detect.Project) (string, error) {
	p.section("THE APP CONTAINER")

	d := specDraft{
		Dockerfile: project.Dockerfile,
		Build:      project.Dockerfile != "",
		Health:     "/",
		Memory:     "512m",
		FromFile:   true,
	}

	// The port comes after the image on purpose: only a Dockerfile being
	// built can speak for it, and that is not settled until the image
	// question is answered.
	err := p.wizard(
		func() error { return askImage(p, &d, project) },
		func() error { return askPort(p, &d, project) },
		func() error { return askHealth(p, &d) },
		func() error { return askMemory(p, &d) },
		func() error { return askFromFile(p, &d, project) },
		func() error { return askServices(p, &d) },
	)
	if err != nil {
		return "", err
	}
	return renderSpec(app, d), nil
}

// askImage is two questions that belong together: naming an image only comes
// up when the answer was not "build it", so backing out of the name returns
// to the choice rather than out of the wizard.
func askImage(p *prompter, d *specDraft, project *detect.Project) error {
	if project.Dockerfile == "" {
		// Nothing here to build, so the image has to come from a registry
		// and there is no choice to offer.
		return askImageName(p, &d.Image)
	}

	return p.wizard(
		func() error {
			def := 1
			if d.Build {
				def = 0
			}
			choice, err := p.choose("image", []string{
				"build from " + project.Dockerfile,
				"pull a published image",
			}, def)
			if err != nil {
				return err
			}
			d.Build = choice == 0
			return nil
		},
		func() error {
			if d.Build {
				return ui.ErrSkip
			}
			return askImageName(p, &d.Image)
		},
	)
}

func askImageName(p *prompter, into *string) error {
	image, err := p.askValid("image", *into, func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("an image name is required, for example nginx:1.29-alpine")
		}
		return nil
	})
	if err != nil {
		return err
	}
	*into = image
	return nil
}

// askPort asks only when the project does not answer for itself.
//
// One EXPOSE in a Dockerfile that is actually being built is an answer, not a
// question. A published image is different: its own EXPOSE cannot be read
// until it is on the machine, and the Dockerfile sitting next to it describes
// something else entirely.
func askPort(p *prompter, d *specDraft, project *detect.Project) error {
	if d.Build && len(project.Ports) == 1 {
		d.Port = strconv.Itoa(project.Ports[0])
		p.say("  " + ui.Good.Render("✓") + " " + dim("port from "+project.Dockerfile+": ") + d.Port)
		return ui.ErrSkip
	}

	if d.Build && len(project.Ports) > 1 {
		options := make([]string, 0, len(project.Ports))
		for _, n := range project.Ports {
			options = append(options, strconv.Itoa(n))
		}
		def := 0
		for i, o := range options {
			if o == d.Port {
				def = i
			}
		}
		choice, err := p.choose(project.Dockerfile+" exposes several ports — which one serves traffic?",
			options, def)
		if err != nil {
			return err
		}
		d.Port = options[choice]
		return nil
	}

	port, err := p.askValid("port the app listens on "+dim("(inside the container)"),
		d.Port, validPort)
	if err != nil {
		return err
	}
	d.Port = port
	return nil
}

func validPort(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("a port between 1 and 65535")
	}
	return nil
}

// askHealth asks how the container says it is up, not just where.
//
// A path is only answerable by a port that speaks HTTP. Plenty of application
// containers do not -- php-fpm listens in FastCGI on 9000 -- and probing one
// with a path fails forever while the container's own log says it is ready.
// Offering the command instead is the difference between a deploy and a
// mystery.
func askHealth(p *prompter, d *specDraft) error {
	return p.wizard(
		func() error {
			kinds := []string{
				fmt.Sprintf("port %s accepts a connection — works whatever it speaks", d.Port),
				fmt.Sprintf("GET a path on port %s — only if it answers in HTTP", d.Port),
				"a command inside the container — the only option with no port",
				"nothing — it counts as up the moment it starts",
			}
			kind, err := p.choose("how does it report being up?", kinds, d.HealthKind)
			if err != nil {
				return err
			}
			d.HealthKind = kind
			return nil
		},
		func() error {
			switch d.HealthKind {
			case healthPath:
				path, err := p.ask("health check path", d.Health)
				if err != nil {
					return err
				}
				if path != "" && !strings.HasPrefix(path, "/") {
					path = "/" + path
				}
				d.Health = path
				return nil

			case healthCmd:
				line, err := p.askValid("command "+dim("(exits 0 when it is up)"),
					strings.Join(d.HealthCmd, " "), func(s string) error {
						if len(strings.Fields(s)) == 0 {
							return fmt.Errorf("a command is required, for example: pg_isready")
						}
						return nil
					})
				if err != nil {
					return err
				}
				d.HealthCmd = strings.Fields(line)
				return nil
			}
			// tcp and none have no detail to ask for.
			return ui.ErrSkip
		},
	)
}

func askMemory(p *prompter, d *specDraft) error {
	memory, err := p.askValid("memory limit", d.Memory, validMemory)
	if err != nil {
		return err
	}
	d.Memory = memory
	return nil
}

func askFromFile(p *prompter, d *specDraft, project *detect.Project) error {
	question := "inject the whole .env into the container?"
	if len(project.EnvKeys) > 0 {
		question = fmt.Sprintf("inject all %d variables of %s into the container?",
			len(project.EnvKeys), project.EnvExample)
	}
	fromFile, err := p.yesNo(question, d.FromFile)
	if err != nil {
		return err
	}
	d.FromFile = fromFile
	return nil
}

// validMemory holds the prompt to the same rule the spec does, so a value
// cannot get past the wizard only to be refused by Docker half way through an
// apply.
func validMemory(s string) error {
	var b spec.Bytes
	if err := (&b).UnmarshalYAML(scalarNode(s)); err != nil {
		return err
	}
	switch {
	case b == 0:
		return fmt.Errorf("a limit is required, for example 512m")
	case b < spec.MinMemory && !hasUnit(s):
		return fmt.Errorf("%s is %s — a bare number is bytes, so write %sm for megabytes",
			strings.TrimSpace(s), b, strings.TrimSpace(s))
	case b < spec.MinMemory:
		return fmt.Errorf("%s is below the %s Docker accepts", b, spec.MinMemory)
	}
	return nil
}

func hasUnit(s string) bool {
	return strings.ContainsAny(strings.ToLower(strings.TrimSpace(s)), "kmgb")
}

// How a service reports being up. The order is the order of the question,
// most-often-right first.
//
// tcp leads because it is the only one that cannot be wrong about the
// protocol, and because the alternative default -- an HTTP path -- quietly
// assumed every application port speaks HTTP. Neither Coolify nor Easypanel
// invents a check either: Coolify makes it opt-in and defers to the image's
// own HEALTHCHECK, Easypanel does not expose one at all.
const (
	healthTCP = iota
	healthPath
	healthCmd
	healthNone
)

type specDraft struct {
	Build      bool
	Dockerfile string
	Image      string
	Port       string
	HealthKind int
	Health     string   // an HTTP path, probed by hploy
	HealthCmd  []string // or a command, which becomes Docker's own healthcheck
	Memory     string
	FromFile   bool
	Services   []preset
}

func askServices(p *prompter, d *specDraft) error {
	p.section("SUPPORT SERVICES")
	p.hint("each one gets a private network alias; none of them publishes a port")

	list := p.lines
	for {
		// The list is redrawn from the draft every time round, so a removal
		// shows up immediately and the questions that built a service are
		// replaced by the one line that describes it.
		p.erase(p.lines - list)
		for _, sv := range d.Services {
			p.say(serviceLine(sv))
		}

		options, def := serviceMenu(d.Services)
		menu := p.lines
		choice, err := p.choose("services", options, def)
		if errors.Is(err, ui.ErrBack) {
			p.erase(p.lines - list)
			return ui.ErrBack
		}
		if err != nil {
			return err
		}
		// The menu is a control, not a record of a decision.
		p.erase(p.lines - menu)

		switch {
		case choice == 0:
			if err := addService(p, d); err != nil && !errors.Is(err, ui.ErrBack) {
				return err
			}
		case choice <= len(d.Services):
			d.Services = append(d.Services[:choice-1], d.Services[choice:]...)
		default:
			return nil
		}
	}
}

// serviceMenu lists what can be done to the draft. Done is the default once
// there is something on the list, because that is the usual next answer.
func serviceMenu(services []preset) ([]string, int) {
	options := []string{"add a service"}
	for _, sv := range services {
		options = append(options, fmt.Sprintf("remove %s  %s", sv.Service, sv.Image))
	}
	options = append(options, "done")
	if len(services) == 0 {
		return options, 0
	}
	return options, len(options) - 1
}

func serviceLine(sv preset) string {
	// Pad the plain text and style afterwards, or the escape codes count
	// toward the width and the column comes out short.
	volume := "no volume"
	if sv.Volume != "" {
		name, path, _ := strings.Cut(sv.Volume, ":")
		volume = name + " at " + path
	}
	return "  " + ui.Good.Render("✓") + " " + dim("service") + "  " +
		ui.Ours.Render(sv.Service+pad(12-len(sv.Service))) + " " +
		sv.Image + pad(24-len(sv.Image)) + " " +
		dim(sv.Memory+" · "+volume)
}

// addService builds one service. Backing out of its first question abandons
// it and leaves the list as it was.
func addService(p *prompter, d *specDraft) error {
	used := usedNames(d.Services)

	var pr preset
	err := p.wizard(
		func() error { return askWhich(p, &pr, used) },
		func() error { return askServiceName(p, &pr, used) },
		func() error {
			memory, err := p.askValid("memory limit", pr.Memory, validMemory)
			if err != nil {
				return err
			}
			pr.Memory = memory
			return nil
		},
		func() error {
			if pr.Volume == "" {
				return ui.ErrSkip
			}
			keep, err := p.yesNo("keep its data in a volume?", true)
			if err != nil {
				return err
			}
			if !keep {
				pr.Volume = ""
			}
			return nil
		},
	)
	if err != nil {
		return err
	}
	commitService(d, pr)
	return nil
}

func usedNames(services []preset) map[string]bool {
	used := map[string]bool{}
	for _, sv := range services {
		used[sv.Service] = true
	}
	return used
}

// commitService puts a finished service on the draft, naming its volume after
// it.
//
// Three presets ship the volume name "dbdata", so a second database would
// otherwise mount the first one's data -- and the generated volumes: block
// would carry the same key twice, which does not even parse.
func commitService(d *specDraft, pr preset) {
	if pr.Volume != "" {
		_, path, _ := strings.Cut(pr.Volume, ":")
		pr.Volume = pr.Service + "data:" + path
	}
	d.Services = append(d.Services, pr)
}

func askWhich(p *prompter, pr *preset, used map[string]bool) error {
	return p.wizard(
		func() error {
			choice, err := p.choose("which", presetLabels(), 0)
			if err != nil {
				return err
			}
			if choice < len(presets) {
				// Replaced wholesale: picking a different preset must not
				// leave the previous one's volume or healthcheck behind.
				*pr = presets[choice]
				pr.Service = uniqueName(pr.Service, used)
				return nil
			}
			*pr = preset{Memory: "256m"}
			return nil
		},
		func() error {
			if pr.Image != "" {
				return ui.ErrSkip
			}
			if err := askImageName(p, &pr.Image); err != nil {
				return err
			}
			pr.Service = uniqueName(defaultServiceName(pr.Image), used)
			return nil
		},
	)
}

func askServiceName(p *prompter, pr *preset, used map[string]bool) error {
	name, err := p.askValid("service name "+dim("(the app reaches it at this name)"),
		pr.Service, func(s string) error {
			if used[s] {
				return fmt.Errorf("%q is already taken", s)
			}
			if s == "web" {
				return fmt.Errorf("%q is the app container's own name", s)
			}
			return nil
		})
	if err != nil {
		return err
	}
	pr.Service = name
	return nil
}

func defaultServiceName(image string) string {
	name := image
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name, _, _ = strings.Cut(name, ":")
	return strings.Trim(reSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

func uniqueName(base string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", base, i)
		if !used[candidate] {
			return candidate
		}
	}
}

// renderSpec writes the YAML by hand rather than marshalling a struct: the
// comments are half the value of a generated file.
func renderSpec(app string, d specDraft) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("# Generated by hploy add on %s.", time.Now().Format("2006-01-02"))
	w("#")
	w("# This file describes the application and holds no secrets, so it belongs")
	w("# in the repository. Domain and credentials are deployment config and live")
	w("# in ~/.hploy instead.")
	w("version: 1")
	w("app: %s", app)
	w("")
	w("web:")
	if d.Build {
		w("  build:")
		w("    context: .")
		if d.Dockerfile != "" && d.Dockerfile != "Dockerfile" {
			w("    dockerfile: %s", d.Dockerfile)
		}
	} else {
		w("  image: %s", d.Image)
	}
	w("  port: %s", d.Port)
	switch {
	case d.HealthKind == healthTCP:
		w("  health: tcp")
	case d.HealthKind == healthPath && d.Health != "":
		w("  health: %s", d.Health)
	case d.HealthKind == healthCmd && len(d.HealthCmd) > 0:
		w("  health:")
		w("    cmd: %s", yamlList(d.HealthCmd))
	}
	w("  resources:")
	w("    memory: %s", d.Memory)
	if d.FromFile {
		w("  env:")
		w("    # every key of the app's env file, which git does not track")
		w("    from_file: true")
	}

	if len(d.Services) > 0 {
		w("")
		w("services:")
		for _, s := range d.Services {
			w("  %s:", s.Service)
			w("    image: %s", s.Image)
			if len(s.Vars) > 0 {
				w("    env:")
				w("      vars:")
				keys := make([]string, 0, len(s.Vars))
				for k := range s.Vars {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					w("        %s: %s", k, s.Vars[k])
				}
			}
			if s.Volume != "" {
				w("    volumes:")
				w("      - %s", s.Volume)
			}
			if len(s.Health) > 0 {
				w("    health:")
				w("      cmd: %s", yamlList(s.Health))
			}
			w("    resources:")
			w("      memory: %s", s.Memory)
		}
	}

	volumes := []string{}
	for _, s := range d.Services {
		if s.Volume == "" {
			continue
		}
		name, _, _ := strings.Cut(s.Volume, ":")
		volumes = append(volumes, name)
	}
	if len(volumes) > 0 {
		w("")
		w("volumes:")
		for _, v := range volumes {
			w("  %s: {}", v)
		}
	}
	return b.String()
}
