//go:build devroots

package license

import "embed"

// rootFiles holds the development roots, whose private halves are public
// test fixtures. Only the tests and release/rehearse.sh build with the
// devroots tag; release/gobuild.sh refuses it outside a rehearsal.
//
//go:embed devroots/root.pub devroots/standby.pub
var rootFiles embed.FS

const rootDir = "devroots"
