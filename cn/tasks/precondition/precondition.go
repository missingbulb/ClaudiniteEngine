// Package precondition is the precondition engine: may this task run now?
// A task declares `preconditions`, a list of conditions all of which must
// hold, `||` between alternatives inside one; this package turns that plus
// the collected signals into a verdict. It fails loud, not closed: an
// unknown term, a malformed argument or an unreadable signal is an error,
// a failed run in the queue's failure lane, never a decline, since a
// decline nobody sees is permanent silence. The same verdict is taken at
// the scheduler's tick and at the executor's pick. Terms, reasons and the
// partial mode are string-identical with the Node engine's
// precondition-policy.mjs at missingbulb/Claudinite@057841ac; the grammar
// and the term vocabulary's static half are shared/taskspec's.
package precondition

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/calendar"
)

// MaxContextItems is how many paths or numbers a term's context names
// before it says how many it dropped.
const MaxContextItems = 40

// The window arithmetic: a task with no run of its own to measure from
// reads its cadence's period, a day where it states none, plus an hour of
// slack.
const (
	Day   = 24 * time.Hour
	Slack = time.Hour
)

// DefaultWindow is the lookback a task reads with no run of its own.
func DefaultWindow(decl taskspec.Decl) time.Duration {
	c := decl.Cadence()
	if c == nil {
		return Day + Slack
	}
	p, _ := calendar.Period(c.Cadence)
	return p + Slack
}

// WindowDays is the lookback in days a verdict is judged over: the
// collected bundle's, else the default.
func WindowDays(decl taskspec.Decl, s Signals) float64 {
	if s.Runs != nil && s.Runs.Window != nil {
		return s.Runs.Window.Days
	}
	return float64(DefaultWindow(decl)) / float64(Day)
}

// Outcome is one term's answer: it holds or not, with a reason and the
// context lines it adds, or it could not answer (Error).
type Outcome struct {
	Holds   bool
	Reason  string
	Context []string
	Error   string
}

// Verdict is the expression's answer. Run is nil while undecided (partial
// mode) or on an error.
type Verdict struct {
	Run       *bool
	Reason    string
	Context   []string
	Undecided bool
	Missing   []string
	Error     string
}

// Go reports a run verdict.
func (v Verdict) Go() bool { return v.Run != nil && *v.Run }

// Declined reports a decline.
func (v Verdict) Declined() bool { return v.Run != nil && !*v.Run && v.Error == "" }

// MarshalJSON writes the Node engine's shape.
func (v Verdict) MarshalJSON() ([]byte, error) {
	switch {
	case v.Error != "":
		return json.Marshal(struct {
			Error string `json:"error"`
		}{v.Error})
	case v.Undecided:
		return json.Marshal(struct {
			Run       *bool    `json:"run"`
			Undecided bool     `json:"undecided"`
			Missing   []string `json:"missing"`
		}{nil, true, nonNil(v.Missing)})
	case v.Go():
		return json.Marshal(struct {
			Run     bool     `json:"run"`
			Reason  string   `json:"reason"`
			Context []string `json:"context"`
		}{true, v.Reason, nonNil(v.Context)})
	}
	return json.Marshal(struct {
		Run    bool   `json:"run"`
		Reason string `json:"reason"`
	}{false, v.Reason})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Opts are what a term reads beyond the signals.
type Opts struct {
	Arg        *string
	Config     map[string]any
	Item       *Item
	WindowDays float64
	Now        *time.Time
}

// Judge answers a task-local term (a preconditions.mjs's, run through the
// Node runner at the tick and at the pick).
type Judge func(ref taskspec.Ref, s Signals, o Opts) Outcome

// Input is one evaluation.
type Input struct {
	Preconditions any
	Signals       Signals
	Config        map[string]any
	Item          *Item
	// Terms are the task's own terms: the engine's request term for the
	// built-in task, a preconditions.mjs's names for a pack's.
	Terms      taskspec.Terms
	Local      Judge
	WindowDays float64
	Now        *time.Time
	Partial    bool
}

// Evaluate judges an expression over collected signals. The conditions
// the engine judges alone are judged first, whatever order the expression
// states them in: a decline there settles the verdict before any
// task-local term is asked, so a task that is not due starts no runner.
// In partial mode a term whose signal has not been collected is unknown
// rather than false, a task-local term is never asked, and the verdict
// is undecided unless some conjunct is already decided false or every
// conjunct held.
func Evaluate(in Input) Verdict {
	conds, bad := taskspec.ParsePreconditions(in.Preconditions)
	if bad != "" {
		return Verdict{Error: `the "preconditions" declaration is not legal: ` + bad}
	}
	local := make([]bool, len(conds))
	for k, alts := range conds {
		for _, ref := range alts {
			spec, ok := taskspec.Resolve(ref.Name, in.Terms)
			if !ok {
				return Verdict{Error: `unknown precondition "` + ref.Name + `" — no built-in and none this task's preconditions.mjs exports`}
			}
			if p := taskspec.ArgumentProblem(spec, ref); p != nil {
				return Verdict{Error: p.What}
			}
			local[k] = local[k] || !EngineJudged(ref.Name)
		}
	}
	type result struct {
		ref taskspec.Ref
		out Outcome
	}
	held := make([]*result, len(conds))
	var missing []string
	declined := ""
	isDeclined := false
	undecided := false
	for _, pass := range []bool{false, true} {
		if pass && isDeclined {
			break
		}
		for k, alts := range conds {
			if local[k] != pass {
				continue
			}
			var outcomes []result
			unknown := false
			for _, ref := range alts {
				spec, _ := taskspec.Resolve(ref.Name, in.Terms)
				if in.Partial && !EngineJudged(ref.Name) {
					unknown = true
					continue
				}
				for _, n := range spec.Signals {
					if _, err := in.Signals.state(n); err != "" {
						return Verdict{Error: ref.Name + ": the `" + n + "` signal could not be read — " + err}
					}
				}
				if in.Partial {
					absent := false
					for _, n := range spec.Signals {
						if present, _ := in.Signals.state(n); !present {
							if !has(missing, n) {
								missing = append(missing, n)
							}
							absent = true
						}
					}
					if absent {
						unknown = true
						continue
					}
				}
				o := Opts{Arg: ref.Arg, Config: in.Config, Item: in.Item, WindowDays: in.WindowDays, Now: in.Now}
				var out Outcome
				if h, builtin := holds[ref.Name]; builtin {
					out = h(in.Signals, o)
				} else if h, engine := engineHolds[ref.Name]; engine {
					out = h(in.Signals, o)
				} else if in.Local != nil {
					out = in.Local(ref, in.Signals, o)
				} else {
					return Verdict{Error: `the precondition "` + ref.Name + `" has no judge here — a task-local term is asked through the runner`}
				}
				if out.Error != "" {
					return Verdict{Error: ref.Name + ": " + out.Error}
				}
				outcomes = append(outcomes, result{ref, out})
			}
			var winner *result
			for i := range outcomes {
				if outcomes[i].out.Holds {
					winner = &outcomes[i]
					break
				}
			}
			switch {
			case winner != nil:
				held[k] = winner
			case unknown:
				undecided = true
			case !isDeclined:
				parts := make([]string, len(outcomes))
				for i, o := range outcomes {
					parts[i] = o.out.Reason
					if parts[i] == "" {
						parts[i] = o.ref.Text + " does not hold"
					}
				}
				declined, isDeclined = strings.Join(parts, "; nor "), true
			}
		}
	}
	f, t := false, true
	if isDeclined {
		return Verdict{Run: &f, Reason: declined}
	}
	if undecided {
		return Verdict{Undecided: true, Missing: missing}
	}
	var reasons, context []string
	for _, w := range held {
		if w == nil {
			continue
		}
		r := w.out.Reason
		if r == "" {
			r = w.ref.Text
		}
		reasons = append(reasons, r)
		context = append(context, w.out.Context...)
	}
	reason := "no conditions stated — the item itself is the ask"
	if len(reasons) > 0 {
		reason = strings.Join(reasons, "; ")
	}
	return Verdict{Run: &t, Reason: reason, Context: context}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func itoa(n int) string { return jsjson.FormatNumber(float64(n)) }
