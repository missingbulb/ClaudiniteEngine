package schedule_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/localterms"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
	"github.com/missingbulb/ClaudiniteEngine/tasks/schedule"
	"github.com/missingbulb/ClaudiniteEngine/tasks/sim"
)

const releaseTerms = `export const terms = {
  'release-due': {
    signals: ['commits'],
    holds: (s) => s.commits.count > 0
      ? { holds: true, reason: 'a commit to release', context: ['Release the commits since the last tag.'] }
      : { holds: false, reason: 'nothing to release' },
  },
  'gate-open': { signals: [], holds: () => ({ holds: true, reason: 'the gate is open' }) },
};
`

func localTask(t *testing.T, preconditions ...any) taskspec.Task {
	t.Helper()
	tk := task("site-release", map[string]any{"preconditions": preconditions})
	tk.Dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(tk.Dir, localterms.File), []byte(releaseTerms), 0o644); err != nil {
		t.Fatal(err)
	}
	tk.Terms = taskspec.TermsFromText(releaseTerms)
	return tk
}

func nodeTerms(t *testing.T) *localterms.Asker {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	dir, err := runner.Unpack(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &localterms.Asker{Runner: runner.Runner{Dir: dir, Engine: "0.0.0-test"}}
}

// noNode is a runner that cannot start: any ask through it is an error.
func noNode() *localterms.Asker {
	return &localterms.Asker{Runner: runner.Runner{Node: "/no/such/node"}}
}

func TestATaskLocalTermThatDeclinesFilesNothing(t *testing.T) {
	h := newHarness(t, localTask(t, "schedule:at-most-daily", "release-due"))
	h.terms = nodeTerms(t)
	out := h.run("")
	if len(h.open()) != 0 || len(out.Asked) != 1 || out.Asked[0].Verdict != schedule.VerdictNo || out.Asked[0].Reason != "nothing to release" {
		t.Fatalf("open %v asked %+v", h.open(), out.Asked)
	}
}

func TestATaskLocalTermThatHoldsFilesTheItemWithItsContext(t *testing.T) {
	h := newHarness(t, localTask(t, "schedule:at-most-daily", "release-due"))
	h.terms = nodeTerms(t)
	h.commit("a1", "feat: a thing", "src/a.go")
	out := h.run("")
	open := h.open()
	if len(open) != 1 || len(out.Asked) != 1 || out.Asked[0].Verdict != schedule.VerdictGo || !strings.Contains(out.Asked[0].Reason, "a commit to release") {
		t.Fatalf("open %v asked %+v", open, out.Asked)
	}
	if !strings.Contains(open[0].Body, "Release the commits since the last tag.") || strings.Contains(open[0].Body, "could not decide") {
		t.Fatalf("body %q", open[0].Body)
	}
}

// A period already covered settles the ask before the task's own terms
// are run, wherever the expression places the cadence: no Node process
// starts for a task that is not due.
func TestAScheduleDeclineRunsNoLocalTerm(t *testing.T) {
	for _, expr := range [][]any{{"schedule:at-most-daily", "release-due"}, {"release-due", "schedule:at-most-daily"},
		{"schedule:at-most-daily", "gate-open"}, {"gate-open", "schedule:at-most-daily"}} {
		h := newHarness(t, localTask(t, expr...))
		h.terms = noNode()
		n := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "[claudinite-work] acme-pack/site-release", Body: "x", Labels: []string{workitem.StatusDone}}})
		_ = h.gh.CloseIssue(n, "completed")
		h.gh.Clock.Advance(time.Hour)
		h.commit("a1", "feat: a thing", "src/a.go")
		out := h.run("")
		if len(h.open()) != 0 || len(out.Asked) != 1 || out.Asked[0].Verdict != schedule.VerdictNo || !strings.Contains(out.Asked[0].Reason, "already ran") {
			t.Errorf("%v: open %v asked %+v", expr, h.open(), out.Asked)
		}
	}
}

// Terms that cannot be asked at all are not a decline: the ask fails
// open, saying why, and the executor decides at the pick.
func TestTermsThatCannotBeAskedFailOpenSayingWhy(t *testing.T) {
	h := newHarness(t, localTask(t, "schedule:at-most-daily", "release-due"))
	h.terms = noNode()
	out := h.run("")
	open := h.open()
	if len(open) != 1 || len(out.Asked) != 1 || out.Asked[0].Verdict != schedule.VerdictFailOpen ||
		!strings.Contains(out.Asked[0].Reason, localterms.CouldNotAsk) {
		t.Fatalf("open %v asked %+v", open, out.Asked)
	}
	if !strings.Contains(open[0].Body, "The scheduler could not decide this occurrence") || !strings.Contains(open[0].Body, localterms.CouldNotAsk) {
		t.Fatalf("body %q", open[0].Body)
	}
}
