//go:build devroots

package trust

import "embed"

// rootFiles adds the development roots, whose private halves are public
// test fixtures, to the ceremony's: the tests and dev/release/verify/rehearse.sh sign
// with the former and read the real pack shelf, signed under the latter.
// dev/build/gobuild.sh refuses the devroots tag outside a rehearsal.
//
//go:embed roots/root.pub roots/standby.pub devroots/root.pub devroots/standby.pub
var rootFiles embed.FS

var rootDirs = []string{"roots", "devroots"}
