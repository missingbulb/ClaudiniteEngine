package execute

import (
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
	"github.com/missingbulb/ClaudiniteEngine/tasks/signals"
)

// LocalTermsFile is the task-local terms module beside a declaration.
const LocalTermsFile = "preconditions.mjs"

// TermsTimeout bounds the one runner call that answers a task's own
// terms.
const TermsTimeout = 2 * time.Minute

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

// localRefs are the references the engine does not judge itself.
func localRefs(t taskspec.Task) []runner.Ref {
	conds, bad := taskspec.ParsePreconditions(t.Decl.Preconditions())
	if bad != "" {
		return nil
	}
	var out []runner.Ref
	seen := map[string]bool{}
	for _, alts := range conds {
		for _, ref := range alts {
			if precondition.EngineJudged(ref.Name) || seen[ref.Text] {
				continue
			}
			if _, local := t.Terms.Get(ref.Name); local {
				seen[ref.Text] = true
				out = append(out, runner.Ref{Name: ref.Name, Arg: ref.Arg, Text: ref.Text})
			}
		}
	}
	return out
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
	window := precondition.WindowDays(t.Decl, s)
	in := precondition.Input{Preconditions: t.Decl.Preconditions(), Signals: s, Config: config, Item: pi,
		Terms: t.Terms, WindowDays: window, Now: &at}
	if refs := localRefs(t); len(refs) > 0 {
		step := runner.Step{Dir: t.Dir, Env: p.Env, Timeout: TermsTimeout, Echo: p.Echo}
		outcomes, err := p.Runner.Terms(step, LocalTermsFile, refs, runner.TermsInput{Signals: s, Config: config,
			Item: facts, WindowDays: window, Now: at.UTC().Format("2006-01-02T15:04:05.000Z")})
		if err != nil {
			return precondition.Verdict{Error: "the task's own terms could not be asked: " + err.Error()}
		}
		in.Local = func(ref taskspec.Ref, _ precondition.Signals, _ precondition.Opts) precondition.Outcome {
			o, ok := outcomes[ref.Text]
			if !ok {
				return precondition.Outcome{Error: LocalTermsFile + " gave no answer for " + ref.Text}
			}
			return precondition.Outcome{Holds: o.Holds, Reason: o.Reason, Context: o.Context, Error: o.Error}
		}
	}
	return precondition.Evaluate(in)
}
