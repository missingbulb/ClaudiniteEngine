// Package report is how the engine reports its own failures: error classes
// with stable codes, the exit code each class maps to, and local crash files.
//
// There is no network reporting. The engine has no channel to us except a
// fleet run's owner check against the license server, and its health model
// is the breadcrumb, counted by the usage extraction over conversation logs
// (docs/design.md, "Engine metrics in the usage extraction"). In GitHub
// Actions a non-zero exit is the report (docs/design.md, "GitHub Actions").
// A crash leaves a file in the cache's crashes/ folder, one stderr line
// naming it, and a crash breadcrumb.
package report
