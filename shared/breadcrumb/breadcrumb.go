// Package breadcrumb formats the fixed-format health line each capability
// leaves in its hook output (docs/design.md, "Health breadcrumbs"):
//
//	[cn] <capability> <event> <outcome> <ms>ms
//
// It carries a marker, the capability, the event, an outcome code and a
// duration, never error text.
package breadcrumb

import (
	"fmt"
	"regexp"
	"time"
)

// Outcome is the closed set of outcome codes.
type Outcome string

const (
	OK      Outcome = "ok"
	Error   Outcome = "error"
	Crash   Outcome = "crash"
	Timeout Outcome = "timeout"
)

var (
	capabilityToken = regexp.MustCompile(`^[a-z]+$`)
	eventToken      = regexp.MustCompile(`^[a-z-]+$`)
)

// Line renders one breadcrumb. A field outside its token set is replaced
// ("unknown", or outcome "error"), so no caller can put free text in it.
func Line(capability, event string, outcome Outcome, d time.Duration) string {
	if !capabilityToken.MatchString(capability) {
		capability = "unknown"
	}
	if !eventToken.MatchString(event) {
		event = "unknown"
	}
	switch outcome {
	case OK, Error, Crash, Timeout:
	default:
		outcome = Error
	}
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("[cn] %s %s %s %dms", capability, event, outcome, ms)
}
