package land

import (
	"github.com/missingbulb/ClaudiniteEngine/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
)

// OutcomeCheck is a run's delivery against its declared ceiling.
type OutcomeCheck struct {
	OK        bool    `json:"ok"`
	Violation *string `json:"violation"`
}

// VerifyOutcome holds what a run did to pull requests to its declared
// ceiling: a no_code_changes task opens and merges none, and a task whose
// automerge authorizes nothing (absent, "nothing" or unreadable) merges
// none. A merge counts as having opened one.
func VerifyOutcome(outcome string, automerge any, openedPR, mergedPR bool) OutcomeCheck {
	fail := func(v string) OutcomeCheck { return OutcomeCheck{Violation: &v} }
	ceiling := taskspec.CanonicalOutcome(outcome)
	if ceiling == "" {
		return fail(`unknown outcome ceiling "` + outcome + `"`)
	}
	if !taskspec.OpensPullRequest(ceiling) && (openedPR || mergedPR) {
		return fail(`a "` + ceiling + `" task must not open or merge a pull request`)
	}
	if mergedPR {
		if automerge == nil {
			automerge = mergepolicy.Nothing
		}
		if k := mergepolicy.Normalize(automerge).Kind; k == mergepolicy.Nothing || k == "invalid" {
			return fail("a task whose automerge authorizes nothing must not merge a pull request")
		}
	}
	return OutcomeCheck{OK: true}
}
