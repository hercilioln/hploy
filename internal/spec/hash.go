package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Hash fingerprints a service's configuration. Stored as a container label,
// it lets apply tell "unchanged" from "needs recreating" without diffing
// against the values Docker normalizes.
//
// Resolved env values are excluded: rotating a secret should not recreate the
// container, and no secret belongs in a label.
func (sv *Service) Hash(app string) string {
	h := sha256.New()

	write := func(format string, args ...any) {
		fmt.Fprintf(h, format+"\x00", args...)
	}

	write("app=%s", app)
	write("service=%s", sv.name)
	write("image=%s", sv.Image)
	write("command=%s", strings.Join(sv.Command, "\x1f"))
	write("port=%d", sv.Port)
	write("memory=%d", uint64(sv.Resources.Memory))
	write("cpus=%s", sv.Resources.CPUs)
	write("health=%s", sv.Health.String())
	write("build=%s", sv.buildFingerprint())

	// Sorted: file order must not change the hash.
	vols := append([]string{}, sv.Volumes...)
	sort.Strings(vols)
	write("volumes=%s", strings.Join(vols, "\x1f"))

	// The RAW templates, not the resolved values: changing ${DB_PASSWORD} to
	// ${OTHER} is a config change, rotating the password behind it is not.
	keys := make([]string, 0, len(sv.Env.Vars))
	for k := range sv.Env.Vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write("var=%s=%s", k, sv.Env.Vars[k])
	}

	required := append([]string{}, sv.Env.Required...)
	sort.Strings(required)
	write("required=%s", strings.Join(required, "\x1f"))

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// buildFingerprint covers how the image is built, not what comes out: a
// changed Dockerfile path or build arg is a config change, while a rebuild of
// the same recipe is caught by comparing image IDs.
func (sv *Service) buildFingerprint() string {
	if sv.Build == nil {
		return "none"
	}
	parts := []string{
		"context=" + sv.Build.ContextOrDefault(),
		"dockerfile=" + sv.Build.Dockerfile,
		"target=" + sv.Build.Target,
	}
	keys := make([]string, 0, len(sv.Build.Args))
	for k := range sv.Build.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, "arg:"+k+"="+sv.Build.Args[k])
	}
	return strings.Join(parts, "\x1f")
}
