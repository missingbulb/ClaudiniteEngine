package execute

import (
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/localterms"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/runner"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/signals"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
)

// Picker asks a task's precondition again at the pick, for one
// occurrence: its signals collected for that item, the engine's terms
// judged here, and every task-local term answered by one runner call.
type Picker struct {
	Collector  *signals.Collector
	PackConfig func(pack string) map[string]any
	Runner     runner.Runner
	Env        []string
	Echo       func(stream, line string)
}

// Evaluate is the verdict for one occurrence. A term that could not
// answer is the verdict's error, never a decline.
func (p Picker) Evaluate(t taskspec.Task, item workitem.Issue, at time.Time) precondition.Verdict {
	if !t.Decl.Has("preconditions") {
		return precondition.Verdict{Error: `the task declares no "preconditions"`}
	}
	var s precondition.Signals
	if p.Collector != nil {
		s = p.Collector.Collect(t, at, &item, nil)
	}
	config := map[string]any{}
	if p.PackConfig != nil {
		if c := p.PackConfig(t.Pack); c != nil {
			config = c
		}
	}
	facts := workitem.ItemFacts(item)
	n := item.Number
	pi := &precondition.Item{Number: &n, Woken: facts.IsWoken, Request: facts.Request}
	return precondition.Evaluate(precondition.Input{Preconditions: t.Decl.Preconditions(), Signals: s, Config: config, Item: pi,
		Terms: t.Terms, Local: localterms.Asker{Runner: p.Runner, Env: p.Env, Echo: p.Echo}.Judge(t, facts),
		WindowDays: precondition.WindowDays(t.Decl, s), Now: &at})
}
