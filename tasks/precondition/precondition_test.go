package precondition

import (
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
)

var monday = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

func sp(s string) *string { return &s }

func run(n int, at time.Time, outcome *string) Run {
	return Run{Number: n, CreatedAt: sp(at.Format(time.RFC3339)), ClosedAt: sp(at.Add(time.Minute).Format(time.RFC3339)), State: "closed", Outcome: outcome}
}

func weekly(runs ...Run) Verdict {
	now := monday.Add(3 * Day)
	return Evaluate(Input{Preconditions: []any{"schedule:at-most-weekly"}, Signals: Signals{Runs: &Runs{List: runs}}, Now: &now})
}

// An item closed as never having run, a decline at its pick, leaves the
// period open; one that ran, or one whose outcome is unknown, covers it.
func TestADeclinedRunCoversNoPeriod(t *testing.T) {
	declined := sp("obsolete")
	chain := []Run{run(1449, monday.Add(2*Day), declined), run(1443, monday.Add(Day), declined), run(1437, monday.Add(time.Hour), declined)}
	if v := weekly(chain...); !v.Go() || !strings.Contains(v.Reason, "no run in the weekly period") {
		t.Errorf("a chain of declines used up the week: %+v", v)
	}
	if v := weekly(run(1432, monday.Add(time.Hour), sp("done"))); !v.Declined() || !strings.Contains(v.Reason, "#1432 already ran") {
		t.Errorf("a done run left the week open: %+v", v)
	}
	if v := weekly(run(1432, monday.Add(time.Hour), nil)); !v.Declined() || !strings.Contains(v.Reason, "#1432 already ran") {
		t.Errorf("a run of unknown outcome left the week open: %+v", v)
	}
	if v := weekly(append(chain, run(1432, monday.Add(time.Hour), sp("done")))...); !v.Declined() || !strings.Contains(v.Reason, "#1432 already ran") {
		t.Errorf("the run that ran, behind the declines, did not cover the week: %+v", v)
	}
}

// A built-in term that declines settles the verdict before any task-local
// term is asked, wherever the expression places it.
func TestABuiltinDeclineAsksNoLocalTerm(t *testing.T) {
	now := monday.Add(3 * Day)
	terms := taskspec.Terms{{Name: "release-due", Signals: []string{}}}
	asked := 0
	local := func(taskspec.Ref, Signals, Opts) Outcome { asked++; return Outcome{Holds: true} }
	ran := Signals{Runs: &Runs{List: []Run{run(7, monday.Add(Day), sp("done"))}}}
	for _, expr := range [][]any{{"release-due", "schedule:at-most-weekly"}, {"schedule:at-most-weekly", "release-due"}} {
		asked = 0
		v := Evaluate(Input{Preconditions: expr, Signals: ran, Terms: terms, Local: local, Now: &now})
		if !v.Declined() || !strings.Contains(v.Reason, "#7 already ran") || asked != 0 {
			t.Errorf("%v: %+v, the local term asked %d time(s)", expr, v, asked)
		}
	}
	asked = 0
	v := Evaluate(Input{Preconditions: []any{"release-due", "schedule:at-most-weekly"}, Signals: Signals{Runs: &Runs{}}, Terms: terms, Local: local, Now: &now})
	if !v.Go() || asked != 1 || v.Reason != "release-due; no run in the weekly period that opened 2026-10-04T00:00:00.000Z" {
		t.Errorf("a due task: %+v, the local term asked %d time(s)", v, asked)
	}
}

// The scheduler's cheap pass never asks a task-local term: it is unknown
// there, and the verdict waits for the full pass.
func TestAPartialPassLeavesALocalTermUnknown(t *testing.T) {
	now := monday.Add(3 * Day)
	terms := taskspec.Terms{{Name: "release-due", Signals: []string{}}}
	v := Evaluate(Input{Preconditions: []any{"release-due", "schedule:at-most-weekly"}, Signals: Signals{Runs: &Runs{}}, Terms: terms, Now: &now, Partial: true})
	if !v.Undecided || v.Error != "" {
		t.Errorf("%+v", v)
	}
}
