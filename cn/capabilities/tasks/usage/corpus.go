package usage

import (
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsregex"
)

// How one session used the corpus: which skills loaded and what made them
// load, which guards fired, what the checks cost. Every counter is a floor:
// the marks are the hooks' own log lines, and a line the transcript lost is
// an under-count.

var hookLineRE = jsPattern(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z) run=(\S+) ([A-Za-z]+): (.+)`)

// HookMark is one hook log line.
type HookMark struct {
	Stamp, Run, Hook, Message string
}

// HookMarks are the hook log lines one entry carries, in order, each line
// once across seen.
func HookMarks(entry any, seen map[string]bool) []HookMark {
	var out []HookMark
	for _, text := range EntryText(entry) {
		for _, m := range hookLineRE.FindAllStringSubmatch(text, -1) {
			if seen[m[0]] {
				continue
			}
			seen[m[0]] = true
			out = append(out, HookMark{Stamp: m[1], Run: m[2], Hook: m[3], Message: jsregex.Trim(m[4])})
		}
	}
	return out
}

var (
	reBlockEdit     = jsPattern(`^done exit=2 skill-not-loaded (\S+) needs (\S+)`)
	reBlockCall     = jsPattern(`^done exit=2 skill-not-loaded-for-call (\S+) needs (\S+)`)
	reGuardBlock    = jsPattern(`^done exit=2 action-guard (\S+)`)
	reGuardAdvisory = jsPattern(`^advisory action-guard (\S+)`)
	reTriggerResult = jsPattern(`^skill-trigger \S+ (\S+)$`)
	reTriggerPrompt = jsPattern(`^skill-trigger (\S+)$`)
)

// Mark is what one hook line says about the corpus: a block, a trigger or
// a guard.
type Mark struct {
	Kind, Cause, Severity string
	Skills, Rules         []string
}

// ReadMark is the corpus mark a hook line is, false where it is none.
func ReadMark(h HookMark) (Mark, bool) {
	if h.Hook == "PreToolUse" {
		if m := reBlockEdit.FindStringSubmatch(h.Message); m != nil {
			return Mark{Kind: "block", Cause: "blockedEdit", Skills: strings.Split(m[2], ",")}, true
		}
		if m := reBlockCall.FindStringSubmatch(h.Message); m != nil {
			return Mark{Kind: "block", Cause: "blockedCall", Skills: strings.Split(m[2], ",")}, true
		}
		if m := reGuardBlock.FindStringSubmatch(h.Message); m != nil {
			return Mark{Kind: "guard", Severity: "blocking", Rules: strings.Split(m[1], ",")}, true
		}
		if m := reGuardAdvisory.FindStringSubmatch(h.Message); m != nil {
			return Mark{Kind: "guard", Severity: "advisory", Rules: strings.Split(m[1], ",")}, true
		}
		return Mark{}, false
	}
	if h.Hook == "PostToolUse" {
		if m := reTriggerResult.FindStringSubmatch(h.Message); m != nil {
			return Mark{Kind: "trigger", Cause: "resultTrigger", Skills: strings.Split(m[1], ",")}, true
		}
	}
	if h.Hook == "UserPromptSubmit" {
		if m := reTriggerPrompt.FindStringSubmatch(h.Message); m != nil {
			return Mark{Kind: "trigger", Cause: "promptTrigger", Skills: strings.Split(m[1], ",")}, true
		}
	}
	return Mark{}, false
}

var reTiming = jsPattern(`claudinite-check-timing v1 (\S+) total=(\d+)((?: ` + nonSpOrEqu + `+=\d+)*)\s*$`)

// Timing is one check-timing record.
type Timing struct {
	Scope   string
	TotalMs float64
	Rules   []RuleTiming
}

// RuleTiming is one rule's milliseconds in a timing record.
type RuleTiming struct {
	ID string
	Ms float64
}

// ParseTiming reads the check runners' timing line, false for any other.
func ParseTiming(text string) (Timing, bool) {
	m := reTiming.FindStringSubmatch(text)
	if m == nil {
		return Timing{}, false
	}
	out := Timing{Scope: m[1], TotalMs: stringToNumber(m[2])}
	if rest := jsregex.Trim(m[3]); rest != "" {
		for _, pair := range splitWS(rest) {
			at := strings.LastIndex(pair, "=")
			out.Rules = append(out.Rules, RuleTiming{ID: pair[:at], Ms: stringToNumber(pair[at+1:])})
		}
	}
	return out, true
}

// CountCheckTiming is what the checks cost a session, keyed <scope> for a
// whole sweep and <scope>/<rule> for a rule in it.
func CountCheckTiming(entries []any) *Obj {
	out := NewObj()
	seen := map[string]bool{}
	add := func(key string, ms float64) {
		row := out.ObjAt(key)
		if row == nil {
			row = ObjOf("runs", 0.0, "totalMs", 0.0, "maxMs", 0.0)
			out.Set(key, row)
		}
		bump(row, "runs", 1)
		bump(row, "totalMs", ms)
		mx, _ := row.Get("maxMs")
		row.Set("maxMs", jsMax(mx, ms))
	}
	for _, entry := range entries {
		for _, h := range HookMarks(entry, seen) {
			rec, ok := ParseTiming(h.Message)
			if !ok {
				continue
			}
			add(rec.Scope, rec.TotalMs)
			for _, r := range rec.Rules {
				add(rec.Scope+"/"+r.ID, r.Ms)
			}
		}
	}
	return out
}

// bump is row[field] += n.
func bump(row *Obj, field string, n float64) {
	v, _ := row.Get(field)
	row.Set(field, plus(v, n))
}

// bumpOr is row[field] = (row[field] ?? 0) + n.
func bumpOr(row *Obj, field string, n any) {
	v, ok := row.Get(field)
	row.Set(field, plus(orZero(v, ok), n))
}

// LoadCauses are why a skill's body entered a session.
var LoadCauses = []string{"voluntary", "blockedEdit", "blockedCall", "resultTrigger", "promptTrigger", "command", "read"}

func zeros(fields []string) *Obj {
	o := NewObj()
	for _, f := range fields {
		o.Set(f, 0.0)
	}
	return o
}

var reSkillMD = regexp.MustCompile(`(?:^|/)skills/([a-z0-9][a-z0-9-]*)/SKILL\.md$`)

func readLoads(entry any, mounted map[string]bool) []string {
	var out []string
	for _, c := range ToolCalls(entry) {
		p, ok := pathString(c.Input, "file_path")
		if c.Name != "Read" || !ok {
			continue
		}
		if m := reSkillMD.FindStringSubmatch(p); m != nil && m[1] != "" && mounted[m[1]] {
			out = append(out, m[1])
		}
	}
	return out
}

// CountToolCalls is every tool call's name, counted.
func CountToolCalls(entries []any) *Obj {
	out := NewObj()
	for _, entry := range entries {
		for _, c := range ToolCalls(entry) {
			bumpOr(out, c.Name, 1.0)
		}
	}
	return out
}

// CorpusUse is the session's skill and guard record.
type CorpusUse struct {
	SkillLoadsBy, SkillBlocks, TriggerFires, GuardFires, ToolCalls *Obj
}

// CountCorpusUse reads the session as one ordered pass, so a load is
// attributed to the nearest earlier mark naming its skill since its last
// load; a typed command or a Read of its SKILL.md names itself, and
// nothing earlier is voluntary.
func CountCorpusUse(entries []any, mounted map[string]bool) CorpusUse {
	out := CorpusUse{SkillLoadsBy: NewObj(), SkillBlocks: NewObj(), TriggerFires: NewObj(), GuardFires: NewObj()}
	seen := map[string]bool{}
	pending := map[string]string{}
	unfollowed := map[string]float64{}
	fires := func(skill string) *Obj {
		row := out.TriggerFires.ObjAt(skill)
		if row == nil {
			row = ObjOf("fired", 0.0, "followed", 0.0)
			out.TriggerFires.Set(skill, row)
		}
		return row
	}
	load := func(skill, cause string) {
		row := out.SkillLoadsBy.ObjAt(skill)
		if row == nil {
			row = zeros(LoadCauses)
			out.SkillLoadsBy.Set(skill, row)
		}
		bump(row, cause, 1)
		if n := unfollowed[skill]; n > 0 {
			bump(fires(skill), "followed", n)
			unfollowed[skill] = 0
		}
		delete(pending, skill)
	}
	causeOr := func(skill, dflt string) string {
		if c, ok := pending[skill]; ok {
			return c
		}
		return dflt
	}
	for _, entry := range entries {
		for _, line := range HookMarks(entry, seen) {
			mark, ok := ReadMark(line)
			if !ok {
				continue
			}
			if mark.Kind == "guard" {
				for _, rule := range mark.Rules {
					row := out.GuardFires.ObjAt(rule)
					if row == nil {
						row = ObjOf("blocking", 0.0, "advisory", 0.0)
						out.GuardFires.Set(rule, row)
					}
					bump(row, mark.Severity, 1)
				}
				continue
			}
			for _, skill := range mark.Skills {
				pending[skill] = mark.Cause
				if mark.Kind == "block" {
					bumpOr(out.SkillBlocks, skill, 1.0)
				} else {
					bump(fires(skill), "fired", 1)
					unfollowed[skill]++
				}
			}
		}
		for _, skill := range SkillToolLoads(entry) {
			load(skill, causeOr(skill, "voluntary"))
		}
		for _, skill := range readLoads(entry, mounted) {
			load(skill, causeOr(skill, "read"))
		}
		if command, ok := CommandName(entry); ok && mounted[command] {
			load(command, causeOr(command, "command"))
		}
	}
	out.ToolCalls = CountToolCalls(entries)
	return out
}

// Declaration is one force-load declaration a moment is counted against:
// a prompt pattern, a tool call or a path.
type Declaration struct {
	Skill, Kind string
	// Path is set for a path declaration, which matches file tools.
	Path bool
	Data any
}

// Hits are the engine's predicates deciding whether a moment hit; a moment
// counter with any of them missing records no key at all.
type Hits struct {
	Path   func(d Declaration, path string) bool
	Call   func(d Declaration, c ToolCall) bool
	Prompt func(d Declaration, text string) bool
}

var fileTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true}

var leadingDotSlash = regexp.MustCompile(`^\.?/`)

// CountMoments is every occasion a triggered skill's declarations named,
// loaded or not.
func CountMoments(entries []any, declarations []Declaration, hits Hits) *Obj {
	out := NewObj()
	if hits.Call == nil || hits.Prompt == nil || hits.Path == nil {
		return out
	}
	hit := func(skill string) { bumpOr(out, skill, 1.0) }
	for _, entry := range entries {
		if text, ok := pathString(entry, "message", "content"); ok && IsUserMessage(entry) {
			for _, d := range declarations {
				if d.Kind == "prompt" && hits.Prompt(d, text) {
					hit(d.Skill)
				}
			}
			continue
		}
		for _, c := range ToolCalls(entry) {
			for _, d := range declarations {
				if d.Kind == "toolCall" {
					if hits.Call(d, c) {
						hit(d.Skill)
					}
					continue
				}
				p, ok := pathString(c.Input, "file_path")
				if !d.Path || !fileTools[c.Name] || !ok {
					continue
				}
				if hits.Path(d, leadingDotSlash.ReplaceAllString(p, "")) {
					hit(d.Skill)
				}
			}
		}
	}
	return out
}
