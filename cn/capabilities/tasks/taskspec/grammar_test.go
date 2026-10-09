package taskspec

import (
	"strings"
	"testing"
)

// A task-local term that shadows a built-in is a redefinition, the
// engine-judged log-past-retention included.
func TestATaskLocalTermShadowingABuiltinIsARedefinition(t *testing.T) {
	for _, name := range []string{"any-commit", LogPastRetention} {
		ps := ValidatePreconditions([]any{name}, Terms{{Name: name}})
		if len(ps) != 1 || !strings.Contains(ps[0].What, `redefines the built-in term "`+name+`"`) {
			t.Errorf("%s: a shadowed built-in went unreported: %+v", name, ps)
		}
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
