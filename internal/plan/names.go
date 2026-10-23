// Package plan compares a spec with what runs on the machine.
//
// It only reads: Build returns the actions that would be taken, and something
// else decides whether to carry them out.
package plan

import "fmt"

// Docker object names are derived, never taken from the spec, so two apps can
// declare a volume called "data" without colliding.
func NetworkName(app string) string { return "hploy_" + app }

func VolumeName(app, volume string) string {
	return fmt.Sprintf("hploy_%s_%s", app, volume)
}

func ContainerName(app, service string) string {
	return fmt.Sprintf("%s-%s", app, service)
}
