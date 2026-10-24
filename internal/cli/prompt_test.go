package cli

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hercilioln/hploy/internal/detect"
	"github.com/hercilioln/hploy/internal/spec"
	"github.com/hercilioln/hploy/internal/store"
	"github.com/hercilioln/hploy/internal/ui"
)

// step records that it ran and answers with what the script says. A nil
// answer means "go back".
func script(log *[]string, name string, answers ...error) func() error {
	i := 0
	return func() error {
		*log = append(*log, name)
		if i < len(answers) {
			err := answers[i]
			i++
			return err
		}
		return nil
	}
}

func TestWizardRunsForward(t *testing.T) {
	var log []string
	p := &prompter{}

	err := p.wizard(
		script(&log, "name"),
		script(&log, "domains"),
		script(&log, "spec"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log, " "); got != "name domains spec" {
		t.Errorf("ran %q", got)
	}
}

// The point of the whole thing: a wrong answer is one key away from being
// asked again, instead of a cancelled wizard.
func TestWizardStepsBack(t *testing.T) {
	var log []string
	p := &prompter{}

	err := p.wizard(
		script(&log, "name"),
		script(&log, "domains"),
		script(&log, "port", ui.ErrBack),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log, " "); got != "name domains port domains port" {
		t.Errorf("ran %q", got)
	}
}

func TestWizardBackOutOfTheFirstStepIsTheCallersCall(t *testing.T) {
	var log []string
	p := &prompter{}

	err := p.wizard(script(&log, "name", ui.ErrBack))
	if !errors.Is(err, ui.ErrBack) {
		t.Errorf("err = %v, want ErrBack so the caller can decide", err)
	}
	// Nothing after it should have run.
	if got := strings.Join(log, " "); got != "name" {
		t.Errorf("ran %q", got)
	}
}

func TestWizardStopsOnARealError(t *testing.T) {
	var log []string
	p := &prompter{}
	boom := fmt.Errorf("the disk is full")

	err := p.wizard(
		script(&log, "name"),
		script(&log, "domains", boom),
		script(&log, "spec"),
	)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(strings.Join(log, " "), "spec") {
		t.Error("a failure must not carry on to the next question")
	}
}

// Going back erases the answer it is about to ask again. The count is how it
// knows how much to erase, so it has to come back down.
func TestWizardKeepsTheLineCountStraight(t *testing.T) {
	p := &prompter{}
	answered := func() func() error {
		return func() error {
			p.lines++ // what an answered question leaves on screen
			return nil
		}
	}
	back := func() error { return ui.ErrBack }

	if err := p.wizard(answered(), answered()); err != nil {
		t.Fatal(err)
	}
	if p.lines != 2 {
		t.Fatalf("lines = %d, want 2", p.lines)
	}

	// Two answered questions, then back to the second, then forward again:
	// still two lines, not three.
	p = &prompter{}
	asked := 0
	second := func() error {
		asked++
		p.lines++
		if asked == 1 {
			p.lines-- // the question that went back rendered nothing
			return back()
		}
		return nil
	}
	if err := p.wizard(answered(), second); err != nil {
		t.Fatal(err)
	}
	if p.lines != 2 {
		t.Errorf("lines = %d, want 2 — the re-asked question must not double up", p.lines)
	}
}

func TestServiceMenuOffersRemoval(t *testing.T) {
	// The failure this exists for: a service typed in wrong used to be a
	// broken spec to fix by hand.
	options, def := serviceMenu([]preset{
		{Service: "db", Image: "postgres:17-alpine"},
		{Service: "no", Image: "no"},
	})

	want := []string{"add a service", "remove db  postgres:17-alpine", "remove no  no", "done"}
	if len(options) != len(want) {
		t.Fatalf("options = %v", options)
	}
	for i := range want {
		if options[i] != want[i] {
			t.Errorf("option %d = %q, want %q", i, options[i], want[i])
		}
	}
	if def != len(options)-1 {
		t.Errorf("default = %d, want done", def)
	}
}

func TestServiceMenuStartsOnAddWhenEmpty(t *testing.T) {
	options, def := serviceMenu(nil)
	if len(options) != 2 || options[0] != "add a service" || options[1] != "done" {
		t.Fatalf("options = %v", options)
	}
	if def != 0 {
		t.Errorf("default = %d, want add a service", def)
	}
}

func generatorFor(t *testing.T, label string) ui.Generator {
	t.Helper()
	for _, g := range generators {
		if strings.Contains(g.Label, label) {
			return g
		}
	}
	t.Fatalf("no generator labelled %q in %v", label, generators)
	return ui.Generator{}
}

// Laravel's parseKey decodes a value only when it starts with "base64:", and
// then Encrypter::supported checks mb_strlen against the cipher -- 32 bytes
// for aes-256-cbc. Both halves have to hold or the app throws
// "Unsupported cipher or incorrect key length" on boot.
func TestLaravelKeyShape(t *testing.T) {
	value := generatorFor(t, "Laravel").New()

	encoded, ok := strings.CutPrefix(value, "base64:")
	if !ok {
		t.Fatalf("%q has no base64: prefix, so Laravel would read it as raw bytes", value)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("%q is not standard base64: %v", encoded, err)
	}
	if len(raw) != 32 {
		t.Errorf("decoded to %d bytes, want the 32 that aes-256-cbc requires", len(raw))
	}
}

// Why the shape has to be a question: the general-purpose secret is a
// perfectly good password and a broken APP_KEY.
func TestUrlSafeSecretWouldNotWorkAsALaravelKey(t *testing.T) {
	value := generatorFor(t, "url-safe").New()

	if strings.HasPrefix(value, "base64:") {
		t.Fatal("the url-safe shape must not pretend to be the Laravel one")
	}
	if len(value) == 32 {
		t.Error("it happens to be 32 bytes raw, which would hide the problem")
	}
	if strings.ContainsAny(value, "+/=") {
		t.Errorf("%q has characters that do not survive a URL or a bare env value", value)
	}
}

func TestHexSecretIs32Bytes(t *testing.T) {
	value := generatorFor(t, "hex").New()

	raw, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("%q is not hex: %v", value, err)
	}
	if len(raw) != 32 {
		t.Errorf("decoded to %d bytes, want 32", len(raw))
	}
}

// Each shape recognises its own output and nobody else's, which is what makes
// regenerating keep the shape an application already accepted.
func TestGeneratorsRecogniseTheirOwnShape(t *testing.T) {
	for i, g := range generators {
		value := g.New()
		if g.Match == nil {
			t.Errorf("%q has no Match, so regenerating it would change shape", g.Label)
			continue
		}
		if !g.Match(value) {
			t.Errorf("%q does not recognise its own %q", g.Label, value)
		}
		for j, other := range generators {
			if i == j || other.Match == nil {
				continue
			}
			if other.Match(value) {
				t.Errorf("%q claims %q, which came from %q", other.Label, value, g.Label)
			}
		}
	}
}

func TestSecretsAreNotRepeated(t *testing.T) {
	for _, g := range generators {
		if a, b := g.New(), g.New(); a == b {
			t.Errorf("%q generated %q twice", g.Label, a)
		}
	}
}

// A generated spec has to parse, and the healthcheck is the part with three
// shapes to get wrong.
func TestRenderedSpecCarriesAnHTTPHealthPath(t *testing.T) {
	sp := parseDraft(t, specDraft{
		Image: "nginx:1.29-alpine", Port: "80", Memory: "128m",
		HealthKind: healthPath, Health: "/up",
	})
	if sp.Web.Health == nil || sp.Web.Health.Path != "/up" {
		t.Fatalf("health = %v", sp.Web.Health)
	}
	if len(sp.Web.Health.Cmd) > 0 {
		t.Error("a path and a command are mutually exclusive")
	}
}

// The php-fpm case: a port that does not speak HTTP is asked, not probed.
func TestRenderedSpecCarriesACommandHealthcheck(t *testing.T) {
	sp := parseDraft(t, specDraft{
		Build: true, Dockerfile: "Dockerfile", Port: "9000", Memory: "512m",
		HealthKind: healthCmd,
		HealthCmd:  []string{"php-fpm-healthcheck", "--timeout", "2"},
	})
	if sp.Web.Health == nil {
		t.Fatal("no healthcheck in the spec")
	}
	if sp.Web.Health.Path != "" {
		t.Errorf("path = %q, want none", sp.Web.Health.Path)
	}
	want := []string{"php-fpm-healthcheck", "--timeout", "2"}
	if strings.Join(sp.Web.Health.Cmd, " ") != strings.Join(want, " ") {
		t.Errorf("cmd = %v, want %v", sp.Web.Health.Cmd, want)
	}
}

func TestRenderedSpecCanHaveNoHealthcheck(t *testing.T) {
	sp := parseDraft(t, specDraft{
		Image: "nginx:1.29-alpine", Port: "80", Memory: "128m",
		HealthKind: healthNone,
		// Left over from an earlier answer: picking "none" has to drop them.
		Health:    "/",
		HealthCmd: []string{"true"},
	})
	if sp.Web.Health != nil {
		t.Errorf("health = %v, want none", sp.Web.Health)
	}
}

func parseDraft(t *testing.T, d specDraft) *spec.Spec {
	t.Helper()
	yaml := renderSpec("demo", d)
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("the generated spec does not parse: %v\n%s", err, yaml)
	}
	return sp
}

// Three presets ship the volume name "dbdata". Two databases in one app used
// to emit it twice, and a YAML mapping with a repeated key does not parse --
// so the wizard was lost after every question had been answered.
func TestTwoDatabasesGetTheirOwnVolume(t *testing.T) {
	var pg, my preset
	for _, p := range presets {
		switch p.Label {
		case "PostgreSQL":
			pg = p
		case "MySQL":
			my = p
		}
	}
	if pg.Volume == "" || my.Volume == "" || pg.Volume != "dbdata:/var/lib/postgresql/data" {
		t.Fatalf("the presets changed shape: %q and %q", pg.Volume, my.Volume)
	}

	d := &specDraft{Image: "nginx", Port: "80", Memory: "128m", HealthKind: healthNone}
	for _, pr := range []preset{pg, my} {
		named := pr
		named.Service = uniqueName(pr.Service, usedNames(d.Services))
		commitService(d, named)
	}

	sp := parseDraft(t, *d)
	if len(sp.Volumes) != 2 {
		t.Fatalf("volumes = %v, want one each", sp.Volumes)
	}
	mounted := map[string]string{}
	for name, sv := range sp.Services {
		if len(sv.Volumes) != 1 {
			t.Fatalf("service %s mounts %v", name, sv.Volumes)
		}
		vol, path, _ := strings.Cut(sv.Volumes[0], ":")
		if other, clash := mounted[vol]; clash {
			t.Errorf("%s and %s both mount %q", name, other, vol)
		}
		mounted[vol] = name
		if path == "" {
			t.Errorf("service %s mounts %q with no path", name, sv.Volumes[0])
		}
	}
}

func TestCommittedNameRefusesARegisteredApp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HPLOY_HOME", home)

	repo := t.TempDir()
	os.WriteFile(filepath.Join(repo, "hploy.yaml"), []byte(`version: 1
app: taken
web:
  image: nginx:1.29-alpine
  port: 80
  resources: {memory: 128m}
`), 0o600)

	project, err := detect.Inspect(repo)
	if err != nil {
		t.Fatal(err)
	}

	// Nothing registered yet: the committed name is simply accepted.
	name, err := committedAppName(project)
	if err != nil || name != "taken" {
		t.Fatalf("name = %q, err = %v", name, err)
	}

	// Registered elsewhere: the name cannot be answered with another one, so
	// refusing beats repointing a deployed app at a second clone.
	os.MkdirAll(store.AppDir("taken"), 0o700)
	if err := store.SaveApp(&store.App{Name: "taken", Path: "/srv/the-real-one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := committedAppName(project); err == nil {
		t.Error("a second clone of the same repo must not take over the registration")
	}

	// And the registration is untouched.
	app, err := store.LoadApp("taken")
	if err != nil {
		t.Fatal(err)
	}
	if app.Path != "/srv/the-real-one" {
		t.Errorf("path = %q", app.Path)
	}
}

func TestRenderedSpecDefaultsToTCP(t *testing.T) {
	// healthTCP is the zero value, so a draft nobody touched asks for the
	// check that cannot be wrong about the protocol.
	var d specDraft
	if d.HealthKind != healthTCP {
		t.Fatalf("HealthKind zero value = %d, want tcp", d.HealthKind)
	}

	sp := parseDraft(t, specDraft{
		Build: true, Dockerfile: "Dockerfile", Port: "9000", Memory: "512m",
		HealthKind: healthTCP,
		// Left over from answers that were stepped back out of.
		Health:    "/",
		HealthCmd: []string{"true"},
	})
	h := sp.Web.Health
	if h == nil || !h.TCP {
		t.Fatalf("health = %#v", h)
	}
	if h.Path != "" || len(h.Cmd) > 0 {
		t.Errorf("an abandoned answer leaked into the spec: %#v", h)
	}
}

// A step with nothing to ask has to carry on the way the wizard was already
// going. Returning nil instead would make it answer instantly on the way
// back and bounce forward again, which looks exactly like esc being broken.
func TestWizardStepsBackThroughASkippedQuestion(t *testing.T) {
	var log []string
	p := &prompter{}

	err := p.wizard(
		script(&log, "image"),
		script(&log, "port", ui.ErrSkip, ui.ErrSkip), // the Dockerfile answered it
		script(&log, "health", ui.ErrBack),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Forward through port, back through it, then forward again.
	if got := strings.Join(log, " "); got != "image port health port image port health" {
		t.Errorf("ran %q", got)
	}
}

func TestWizardSkippingTheFirstStepGoesBackToTheCaller(t *testing.T) {
	var log []string
	p := &prompter{}

	err := p.wizard(
		script(&log, "port", ui.ErrSkip, ui.ErrSkip),
		script(&log, "health", ui.ErrBack),
	)
	if !errors.Is(err, ui.ErrBack) {
		t.Errorf("err = %v, want ErrBack so the caller decides", err)
	}
}

func TestWizardSkipsForwardWithoutAsking(t *testing.T) {
	var log []string
	p := &prompter{}

	if err := p.wizard(
		script(&log, "a"),
		script(&log, "skipped", ui.ErrSkip),
		script(&log, "b"),
	); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log, " "); got != "a skipped b" {
		t.Errorf("ran %q", got)
	}
}

// The prompt holds to the same floor as the spec, so a value cannot get past
// the wizard only to be refused by Docker half way through an apply.
func TestValidMemoryMatchesTheSpec(t *testing.T) {
	if err := validMemory("256"); err == nil {
		t.Fatal("256 bytes was accepted at the prompt")
	} else if !strings.Contains(err.Error(), "write 256m") {
		t.Errorf("the error does not say what to type instead:\n%v", err)
	}

	for _, bad := range []string{"0", "5", "1m", "nonsense"} {
		if validMemory(bad) == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, good := range []string{"6m", "128m", "512m", "1g"} {
		if err := validMemory(good); err != nil {
			t.Errorf("%q was rejected: %v", good, err)
		}
	}
}

func TestHasUnitTellsBytesFromMegabytes(t *testing.T) {
	for _, with := range []string{"512m", "2G", " 64mb ", "1k"} {
		if !hasUnit(with) {
			t.Errorf("%q has a unit", with)
		}
	}
	for _, without := range []string{"256", " 6291456", "0"} {
		if hasUnit(without) {
			t.Errorf("%q has no unit", without)
		}
	}
}
