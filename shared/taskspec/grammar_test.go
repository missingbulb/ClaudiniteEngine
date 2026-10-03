package taskspec

import (
	"strings"
	"testing"
)

// A pack older than the engine that adopted its term still exports it from
// the task's preconditions.mjs; the engine judges the term and reports no
// redefinition, while any other built-in shadowed stays a problem.
func TestAnAdoptedTermExportedByAnOlderPackIsNoRedefinition(t *testing.T) {
	local := Terms{{Name: LogPastRetention}}
	if ps := ValidatePreconditions([]any{LogPastRetention}, local); len(ps) != 0 {
		t.Errorf("an adopted term's export was reported: %+v", ps)
	}
	ps := ValidatePreconditions([]any{"any-commit"}, Terms{{Name: "any-commit"}})
	if len(ps) != 1 || !strings.Contains(ps[0].What, `redefines the built-in term "any-commit"`) {
		t.Errorf("a shadowed built-in went unreported: %+v", ps)
	}
}
