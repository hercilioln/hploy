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
// Env values are excluded, only their names count: rotating a secret should
// not recreate the container, and no secret belongs in a label.
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

	// Sorted: file order must not change the hash.
	vols := append([]string{}, sv.Volumes...)
	sort.Strings(vols)
	write("volumes=%s", strings.Join(vols, "\x1f"))

	write("env=%s", strings.Join(sv.EnvNames(), "\x1f"))

	// Literals are configuration, so their values count.
	keys := make([]string, 0, len(sv.Env.Literal))
	for k := range sv.Env.Literal {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write("literal=%s=%s", k, sv.Env.Literal[k])
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}
