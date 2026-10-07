// Package checks is the check engine's front: it finds the declared packs'
// Go checks, has the checks binary built (checks/build) and runs a tag
// slice of it (checks/run). Declarative checks and the engine's built-in
// checks join it in phase 6.
package checks
