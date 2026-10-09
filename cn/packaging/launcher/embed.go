// Package launcher holds the member launcher, cn/packaging/launcher/launch, embedded so
// cn verify can tell a member's .claudinite/launch from anything but a
// launcher this major shipped.
package launcher

import (
	_ "embed"
	"strings"
)

// Script is the launcher this engine version ships.
//
//go:embed launch
var Script []byte

//go:embed shipped.sha256
var shipped string

// Shipped is the SHA-256, in hex, of every launcher an earlier release of
// this major shipped that differs from Script.
func Shipped() []string {
	var out []string
	for _, l := range strings.Split(shipped, "\n") {
		if f := strings.Fields(l); len(f) > 0 && !strings.HasPrefix(f[0], "#") {
			out = append(out, f[0])
		}
	}
	return out
}
