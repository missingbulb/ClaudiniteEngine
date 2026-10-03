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

// A path-prefix term matches its argument literally, so a glob in it can
// match nothing: the contract refuses one in every path-prefix term, in a
// string entry and inside an alternation alike, and passes a literal prefix.
func TestAPathPrefixTermRefusesAWildcard(t *testing.T) {
	for _, pre := range []any{
		[]any{"commits-under:.claudinite/settings.*"},
		[]any{"mount-moved || commits-under:.claudinite/settings.*"},
		[]any{"commits-outside:docs/*"},
		[]any{"no-open-pr-touching:packs/*/RULES.md"},
	} {
		ps := ValidatePreconditions(pre, nil)
		if len(ps) != 1 || !strings.Contains(ps[0].What, "a literal path prefix") {
			t.Errorf("%v: %+v", pre, ps)
		}
	}
	if ps := ValidatePreconditions([]any{"mount-moved || commits-under:.claudinite/settings."}, nil); len(ps) != 0 {
		t.Errorf("a literal prefix was refused: %+v", ps)
	}
}
