package taskspec

import (
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsregex"
)

// The precondition grammar's words. The list is a conjunction; `||`
// separates the alternatives inside one entry; a term's inline argument
// follows its first colon.
const (
	Alternative   = "||"
	ScheduleTerm  = "schedule"
	AtMostPrefix  = "at-most-"
	DueTerm       = "due"
	NotFailedTerm = "last-run-not-failed"
	NotParkedTerm = "last-run-not-parked"
	// None is the retired empty precondition.
	None = "none"
)

// Cadences are the periods a cadence term names.
var Cadences = []string{"daily", "weekly", "monthly"}

// Frequencies are the retired `frequency` field's values, read only to
// name the term that replaces them.
var Frequencies = []string{"daily", "weekly", "monthly", "manual"}

// ScheduleTermFor is the term a cadence is stated as.
func ScheduleTermFor(cadence string) string { return ScheduleTerm + ":" + AtMostPrefix + cadence }

// CadenceOfScheduleArg is the cadence a schedule term's argument names,
// or "" when it names none.
func CadenceOfScheduleArg(arg string) string {
	c, ok := strings.CutPrefix(arg, AtMostPrefix)
	if ok && has(Cadences, c) {
		return c
	}
	return ""
}

// cadenceTermFor is what a retired frequency meant, as its term, or ""
// for manual, which meant no schedule at all.
func cadenceTermFor(frequency string) string {
	if frequency == "manual" {
		return ""
	}
	return ScheduleTermFor(frequency)
}

// NormalizeCadenceTerms rewrites every `due:<cadence>` in an expression to
// the current spelling, leaving everything else byte-identical. A value
// that is not a list passes through.
func NormalizeCadenceTerms(pre any) any {
	list, ok := pre.([]any)
	if !ok {
		return pre
	}
	out := make([]any, len(list))
	for i, entry := range list {
		s, isStr := entry.(string)
		if !isStr {
			out[i] = entry
			continue
		}
		alts := strings.Split(s, Alternative)
		for j, alt := range alts {
			t := jsregex.Trim(alt)
			if c, ok := strings.CutPrefix(t, DueTerm+":"); ok && has(Cadences, c) {
				alts[j] = strings.Replace(alt, t, ScheduleTermFor(c), 1)
			}
		}
		out[i] = strings.Join(alts, Alternative)
	}
	return out
}

// Ref is one term reference: its name and the inline argument after the
// first colon (nil when there is no colon).
type Ref struct {
	Name string
	Arg  *string
	Text string
}

func parseTerm(text string) Ref {
	raw := jsregex.Trim(text)
	name, arg, found := strings.Cut(raw, ":")
	if !found {
		return Ref{Name: raw, Text: raw}
	}
	a := jsregex.Trim(arg)
	return Ref{Name: jsregex.Trim(name), Arg: &a, Text: raw}
}

// ParsePreconditions parses a declaration into its conditions, each a list
// of alternatives, or returns the reason it is not legal.
func ParsePreconditions(pre any) ([][]Ref, string) {
	list, ok := pre.([]any)
	if !ok {
		if strs, isStrs := pre.([]string); isStrs {
			list = make([]any, len(strs))
			for i, s := range strs {
				list[i] = s
			}
		} else {
			return nil, "it is not an array of condition strings"
		}
	}
	for _, e := range list {
		s, isStr := e.(string)
		if !isStr || jsregex.Trim(s) == "" {
			return nil, "every entry must be a non-empty string"
		}
	}
	conds := make([][]Ref, len(list))
	for i, e := range list {
		for _, alt := range strings.Split(e.(string), Alternative) {
			conds[i] = append(conds[i], parseTerm(alt))
		}
	}
	for _, alts := range conds {
		for _, t := range alts {
			if t.Name == "" {
				return nil, `an alternative around "` + Alternative + `" is empty`
			}
		}
	}
	for _, alts := range conds {
		for _, t := range alts {
			if t.Name == None {
				return nil, `"` + None + `" is retired. A task with no condition states none: leave "preconditions" out, and the task runs only from an item somebody creates. Otherwise state when it runs: a cadence (` + "`" + ScheduleTermFor("daily") + "`" + `) or the movement it waits for`
			}
		}
	}
	return conds, ""
}

// entriesOf is the looser split the cadence readers use: any entry's
// string form, empty alternatives dropped.
func entriesOf(pre any) [][]Ref {
	list, _ := pre.([]any)
	out := make([][]Ref, 0, len(list))
	for _, e := range list {
		text := ""
		if e != nil {
			text = jsjson.StringOf(e)
		}
		var alts []Ref
		for _, t := range strings.Split(text, Alternative) {
			if t = jsregex.Trim(t); t != "" {
				alts = append(alts, parseTerm(t))
			}
		}
		out = append(out, alts)
	}
	return out
}

// Cadence is the period a declaration states, as {kind: "period",
// cadence}.
type Cadence struct {
	Kind    string `json:"kind"`
	Cadence string `json:"cadence"`
}

// CadenceOf is the first cadence term's period, or nil when the
// declaration states none. Both spellings are read.
func CadenceOf(pre any) *Cadence {
	for _, alts := range entriesOf(pre) {
		for _, ref := range alts {
			arg := ""
			if ref.Arg != nil {
				arg = *ref.Arg
			}
			if ref.Name == ScheduleTerm {
				if c := CadenceOfScheduleArg(arg); c != "" {
					return &Cadence{"period", c}
				}
			}
			if ref.Name == DueTerm && has(Cadences, arg) {
				return &Cadence{"period", arg}
			}
		}
	}
	return nil
}

func gatesOn(pre any, term string) bool {
	for _, alts := range entriesOf(pre) {
		if len(alts) == 1 && alts[0].Name == term && alts[0].Arg == nil {
			return true
		}
	}
	return false
}

// HoldsOnFailure reports whether the task stops past its own failure park.
func HoldsOnFailure(pre any) bool { return gatesOn(pre, NotFailedTerm) || gatesOn(pre, NotParkedTerm) }

// HoldsOnAnyPark reports whether the task stops past every park.
func HoldsOnAnyPark(pre any) bool { return gatesOn(pre, NotParkedTerm) }

// TermSpec is what a term is, apart from how it judges: the signals it
// reads, whether it takes an argument (and which), whether it reads the
// item.
type TermSpec struct {
	Name      string
	Signals   []string
	TakesArg  bool
	NeedsItem bool
	ArgName   string
	ArgHint   string
	ArgOk     func(string) bool
}

// Terms is an ordered term vocabulary.
type Terms []TermSpec

// Get is the term named name.
func (ts Terms) Get(name string) (TermSpec, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	return TermSpec{}, false
}

// Names are the terms' names, in order.
func (ts Terms) Names() []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}

func atMostArgs() []string {
	out := make([]string, len(Cadences))
	for i, c := range Cadences {
		out[i] = AtMostPrefix + c
	}
	return out
}

// BuiltinTerms are the conditions every repo shares, in the Node engine's
// order; tasks/precondition holds how each judges.
var BuiltinTerms = Terms{
	{Name: ScheduleTerm, Signals: []string{"runs"}, TakesArg: true, ArgName: strings.Join(atMostArgs(), "|"),
		ArgOk: func(a string) bool { return CadenceOfScheduleArg(a) != "" }, ArgHint: "one of " + strings.Join(atMostArgs(), ", ")},
	{Name: DueTerm, Signals: []string{"runs"}, TakesArg: true, ArgName: strings.Join(Cadences, "|"),
		ArgOk: func(a string) bool { return has(Cadences, a) }, ArgHint: "one of " + strings.Join(Cadences, ", ")},
	{Name: NotFailedTerm, Signals: []string{"runs"}},
	{Name: NotParkedTerm, Signals: []string{"runs"}},
	{Name: "repo-active", Signals: []string{"commits", "issues", "prs", "conversationLogs"}},
	{Name: "substantive-change", Signals: []string{"commits"}},
	{Name: "any-commit", Signals: []string{"commits"}},
	{Name: "session-captured", Signals: []string{"conversationLogs"}},
	{Name: "issues-touched", Signals: []string{"issues"}},
	{Name: "prs-touched", Signals: []string{"prs"}},
	{Name: "mount-moved", Signals: []string{"sharedMount"}},
	{Name: "commits-under", Signals: []string{"commits"}, TakesArg: true, ArgName: "path-prefix"},
	{Name: "commits-outside", Signals: []string{"commits"}, TakesArg: true, ArgName: "path-prefix"},
	{Name: "no-open-pr-touching", Signals: []string{"prs"}, TakesArg: true, ArgName: "path-prefix"},
	{Name: "no-open-pr-titled", Signals: []string{"prs"}, TakesArg: true, ArgName: "title-prefix"},
	{Name: LogPastRetention, Signals: []string{"conversationLogs"}},
}

// LogPastRetention holds when the conversation-logs branch's oldest
// capture is older than the repo's retention: a clock crossing a
// boundary, which no movement term can say.
const LogPastRetention = "log-past-retention"

// RequestEligible is the engine's own request task's term: about one named
// issue, so it reads the item.
const RequestEligible = "request-eligible"

// EngineTerms are the terms only the engine's built-in task may name.
var EngineTerms = Terms{{Name: RequestEligible, Signals: []string{"request"}, NeedsItem: true}}

// Resolve is a term by name: the built-ins first, then the task's own.
func Resolve(name string, task Terms) (TermSpec, bool) {
	if t, ok := BuiltinTerms.Get(name); ok {
		return t, true
	}
	return task.Get(name)
}

// Signals are every signal the expression's terms read, in first-read
// order.
func Signals(pre any, task Terms) []string {
	conds, bad := ParsePreconditions(pre)
	if bad != "" {
		return []string{}
	}
	out := []string{}
	for _, alts := range conds {
		for _, ref := range alts {
			t, _ := Resolve(ref.Name, task)
			for _, s := range t.Signals {
				if !has(out, s) {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// NeedsItem reports whether the expression reads the item itself.
func NeedsItem(pre any, task Terms) bool {
	conds, bad := ParsePreconditions(pre)
	if bad != "" {
		return false
	}
	for _, alts := range conds {
		for _, ref := range alts {
			if t, ok := Resolve(ref.Name, task); ok && t.NeedsItem {
				return true
			}
		}
	}
	return false
}

// ArgumentProblem judges a term's argument statically: present where it
// takes one, absent where it does not, and legal where it names its legal
// values.
func ArgumentProblem(t TermSpec, ref Ref) *Problem {
	name := t.ArgName
	if name == "" {
		name = "value"
	}
	switch {
	case t.TakesArg && (ref.Arg == nil || *ref.Arg == ""):
		return &Problem{`the precondition "` + ref.Name + `" takes an inline argument and was given none`, `write it as "` + ref.Name + `:<` + name + `>"`}
	case !t.TakesArg && ref.Arg != nil:
		return &Problem{`the precondition "` + ref.Name + `" takes no argument but was given "` + *ref.Arg + `"`, `write it as "` + ref.Name + `"`}
	case t.TakesArg && t.ArgOk != nil && !t.ArgOk(*ref.Arg):
		return &Problem{`"` + ref.Name + `" takes ` + t.ArgHint + `, not "` + *ref.Arg + `"`, `write it as "` + ref.Name + `:<` + name + `>" — ` + t.ArgHint}
	}
	return nil
}

// ValidatePreconditions is everything about an expression decidable
// without signals: the grammar, the names, their arguments, and a
// task-local term shadowing a built-in.
func ValidatePreconditions(pre any, task Terms) []Problem {
	var problems []Problem
	builtins := strings.Join(BuiltinTerms.Names(), ", ")
	for _, t := range task {
		if _, ok := BuiltinTerms.Get(t.Name); ok {
			problems = append(problems, Problem{`the task's preconditions.mjs redefines the built-in term "` + t.Name + `"`,
				"rename the task-local term — the term namespace is flat, and the built-ins are: " + builtins})
		}
	}
	conds, bad := ParsePreconditions(pre)
	if bad != "" {
		return append(problems, Problem{`"preconditions" is not a legal expression: ` + bad,
			`write a list of conditions, all of which must hold, a cadence first where the task keeps one ("` + ScheduleTerm + ":" + AtMostPrefix + `<daily|weekly|monthly>"), then what it waits for, e.g. ["` + ScheduleTermFor("weekly") + `", "substantive-change", "no-open-pr-titled:My sweep"]; a task that runs only from an item somebody creates states no "preconditions" at all`})
	}
	for _, alts := range conds {
		for _, ref := range alts {
			t, ok := Resolve(ref.Name, task)
			if !ok {
				problems = append(problems, Problem{`"preconditions" names the unknown condition "` + ref.Name + `"`,
					"use a built-in (" + builtins + ") or a term this task's preconditions.mjs exports"})
				continue
			}
			if p := ArgumentProblem(t, ref); p != nil {
				problems = append(problems, *p)
			}
		}
	}
	return problems
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
