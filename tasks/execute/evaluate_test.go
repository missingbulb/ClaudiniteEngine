package execute

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
	"github.com/missingbulb/ClaudiniteEngine/tasks/signals"
	"github.com/missingbulb/ClaudiniteEngine/tasks/sim"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

func gatedTask(t *testing.T, preconditions []any, module string) taskspec.Task {
	t.Helper()
	tk := loopTask("a", map[string]any{"preconditions": preconditions})
	tk.Dir = t.TempDir()
	if module != "" {
		_ = os.WriteFile(filepath.Join(tk.Dir, LocalTermsFile), []byte(module), 0o644)
		tk.Terms = taskspec.TermsFromText(module)
	}
	return tk
}

// A task naming only the engine's terms never starts the runner.
func TestAnEngineOnlyExpressionAsksNoRunner(t *testing.T) {
	tk := gatedTask(t, []any{"schedule:at-most-daily"}, "")
	p := Picker{Runner: runner.Runner{Node: "/no/such/node"}}
	v := p.Evaluate(tk, workitem.Issue{Number: 4, Title: "[claudinite-work] acme-pack/a"}, loopNow)
	if v.Error != "" || !v.Go() {
		t.Errorf("%+v", v)
	}
}

func TestTheTasksOwnTermsAreAskedOnceThroughTheRunner(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	dir, err := runner.Unpack(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	module := `export const terms = {
  'my-gate': { signals: [], holds: (_s, o) => ({ holds: o.item.number === 4 && o.item.woken, reason: 'gate saw #' + o.item.number, context: ['ctx line'] }) },
  'my-no': { signals: [], holds: () => ({ holds: false, reason: 'nothing to do' }) },
  'boom': { signals: [], holds: () => { throw new Error('boom'); } },
};
`
	p := Picker{Runner: runner.Runner{Dir: dir, Engine: "0.0.0-test"}}
	woken := workitem.Issue{Number: 4, Title: "[claudinite-work] acme-pack/a probe"}
	if v := p.Evaluate(gatedTask(t, []any{"my-gate"}, module), woken, loopNow); !v.Go() || strings.Join(v.Context, "|") != "ctx line" {
		t.Errorf("%+v", v)
	}
	if v := p.Evaluate(gatedTask(t, []any{"my-gate", "my-no"}, module), woken, loopNow); !v.Declined() || v.Reason != "nothing to do" {
		t.Errorf("%+v", v)
	}
	if v := p.Evaluate(gatedTask(t, []any{"boom"}, module), woken, loopNow); v.Error == "" || !strings.Contains(v.Error, "threw: boom") {
		t.Errorf("a throw is an error, never a decline: %+v", v)
	}
	broken := gatedTask(t, []any{"my-gate"}, module)
	_ = os.WriteFile(filepath.Join(broken.Dir, LocalTermsFile), []byte("export const terms = {;\n"), 0o644)
	if v := p.Evaluate(broken, woken, loopNow); v.Error == "" || !strings.Contains(v.Error, "did not load") {
		t.Errorf("%+v", v)
	}
}

// A logs-prune item runs at the pick only once the branch's oldest
// capture is past the repo's retention, judged by the engine alone.
func TestALogsPruneItemGoesOnlyPastRetention(t *testing.T) {
	tk := loopTask("logs-prune", map[string]any{"trigger": "request", "preconditions": []any{"log-past-retention"},
		"code_work": "cn growth prune", "expected_outcome": "no_code_changes"})
	repo := sim.NewRepo()
	repo.Branches = []world.Branch{{Name: "conversation-logs"}}
	item := workitem.Issue{Number: 9, Title: "[claudinite-work] acme-pack/logs-prune"}
	at := func(days float64) precondition.Verdict {
		stamp := loopNow.Add(-time.Duration(days * 24 * float64(time.Hour)))
		repo.Trees["conversation-logs"] = []string{"README.md", stamp.Format("2006-01-02T1504Z") + "--pr-1--s.jsonl"}
		p := Picker{Collector: &signals.Collector{Issues: sim.NewGitHub(sim.NewClock(loopNow)), Repo: repo, DefaultBranch: "main"},
			Runner: runner.Runner{Node: "/no/such/node"}}
		return p.Evaluate(tk, item, loopNow)
	}
	if v := at(9); !v.Declined() || !strings.Contains(v.Reason, "no log older than retention 10d") {
		t.Fatalf("inside the window: %+v", v)
	}
	if v := at(11); !v.Go() || !strings.Contains(v.Reason, "vs retention 10d") {
		t.Fatalf("past the window: %+v", v)
	}
	repo.Branches = nil
	if v := at(11); !v.Declined() {
		t.Fatalf("no branch: %+v", v)
	}
}
