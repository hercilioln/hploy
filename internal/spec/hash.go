package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Hash fingerprints a service's configuration, including the environment it
// will be handed. Stored as a container label, it lets apply tell "unchanged"
// from "needs recreating" without diffing against the values Docker
// normalizes.
//
// The values are in the hash because a container's environment is fixed when
// it is created. A value that changed without recreating the container is a
// value that never took effect -- so excluding them, as this once did, made
// rotating a password a silent no-op.
//
// Only the digest reaches the label, and it covers the whole configuration at
// once, so no value can be read back out of it.
func (sv *Service) Hash(app string, env map[string]string) string {
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

	// from_file on its own as well as the result: flipping it with an empty
	// env file changes nothing injected, but it is still a config change.
	write("from_file=%t", sv.Env.FromFile)

	envKeys := make([]string, 0, len(env))
	for k := range env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		write("env=%s=%s", k, env[k])
	}

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
