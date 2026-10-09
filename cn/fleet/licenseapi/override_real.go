//go:build !devroots

package licenseapi

// baseOverride is empty in a release build, which always talks to
// DefaultBase.
const baseOverride = ""
