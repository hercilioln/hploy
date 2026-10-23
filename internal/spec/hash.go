package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Hash boils a service's configuration down to a short string.
//
// This is what makes `apply` idempotent without comparing the desired config
// field by field against what Docker returns -- a comparison that is misery,
// because the daemon normalizes values (env order, volume paths, memory units)
// and a naive diff would report differences that are not there.
//
// Instead we store the hash as a container label. On the next apply:
//
//	same hash      -> nothing to do
//	different hash -> recreate
//
// The hash does NOT include env values, only their names: changing a password
// should not force a container recreate (that is deploy's job, not apply's),
// and a secret has no business inside an identifier that sits in a visible
// Docker label.
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

	// Volumes and env sorted: the order in the file must not change the hash.
	vols := append([]string{}, sv.Volumes...)
	sort.Strings(vols)
	write("volumes=%s", strings.Join(vols, "\x1f"))

	write("env=%s", strings.Join(sv.EnvNames(), "\x1f"))

	// Literals ARE configuration, not secrets: they go in with their values.
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
