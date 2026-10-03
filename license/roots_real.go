//go:build !devroots

package license

import "embed"

// rootFiles holds the roots of the key ceremony (#5), the only roots a
// released cn trusts.
//
//go:embed roots/root.pub roots/standby.pub
var rootFiles embed.FS

const rootDir = "roots"
