//go:build devroots

package licenseapi

// baseOverride is the variable a development build reads the license
// server's base from, so the rehearsal reaches its stub.
const baseOverride = "CLAUDINITE_LICENSE_API"
