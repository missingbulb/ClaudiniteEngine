// Package localterms asks a task's own precondition terms, the ones its
// preconditions.mjs exports, through the Node runner. Both sides that
// take a verdict ask through it: the scheduler at its tick and the
// executor at its pick.
package localterms

import (
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/runner"
)

// File is the task-local terms module beside a declaration.
const File = "preconditions.mjs"

// Timeout bounds the one runner call that answers a task's own terms.
const Timeout = 2 * time.Minute

// CouldNotAsk leads the error of a verdict whose terms module could not
// be run at all.
const CouldNotAsk = "the task's own terms could not be asked: "

// Asker runs a task's terms module: the runner, the environment the
// module runs with, and where its output goes.
type Asker struct {
	Runner runner.Runner
	Env    []string
	Echo   func(stream, line string)
}

// Refs are the references in a task's expression the engine does not
// judge itself, each once.
func Refs(t taskspec.Task) []runner.Ref {
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

// Judge is the task's local judge for one verdict. The first term it is
// asked starts one runner call answering every local reference, over the
// bundle and options that term was asked with; the rest read that call's
// answers. A verdict that never reaches a local term starts no process.
// item is what the module reads as the item under evaluation, nil for
// none.
func (a Asker) Judge(t taskspec.Task, item any) precondition.Judge {
	var outcomes map[string]runner.Outcome
	var askErr error
	asked := false
	return func(ref taskspec.Ref, s precondition.Signals, o precondition.Opts) precondition.Outcome {
		if !asked {
			asked = true
			now := ""
			if o.Now != nil {
				now = o.Now.UTC().Format("2006-01-02T15:04:05.000Z")
			}
			step := runner.Step{Dir: t.Dir, Env: a.Env, Timeout: Timeout, Echo: a.Echo}
			outcomes, askErr = a.Runner.Terms(step, File, Refs(t), runner.TermsInput{Signals: s, Config: o.Config,
				Item: item, WindowDays: o.WindowDays, Now: now})
		}
		if askErr != nil {
			return precondition.Outcome{Error: CouldNotAsk + askErr.Error()}
		}
		out, ok := outcomes[ref.Text]
		if !ok {
			return precondition.Outcome{Error: File + " gave no answer for " + ref.Text}
		}
		return precondition.Outcome{Holds: out.Holds, Reason: out.Reason, Context: out.Context, Error: out.Error}
	}
}
