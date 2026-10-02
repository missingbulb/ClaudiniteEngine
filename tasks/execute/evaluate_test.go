package execute

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
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
