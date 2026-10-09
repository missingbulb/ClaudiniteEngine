package usage

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsregex"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/queue"
)

// Counting one capture file. The checks are read off the three marks they
// leave in a transcript (the Stop hook's completion line, the findings
// summary and a finding's header) and the runner invocations a Bash
// command names; a CI job log the session fetched counts its own printed
// runs. Every count is a floor on what ran, never an over-count.

var hookDoneRE = jsPattern(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z) run=(\S+) Stop: done exit=(\d+) ?([a-z-]*)`)

// HookRun is one Stop hook completion line.
type HookRun struct {
	Stamp  string
	Exit   float64
	Reason string
}

// HookCheckRuns are the Stop hook completion lines a text carries.
func HookCheckRuns(text string) []HookRun {
	var out []HookRun
	for _, m := range hookDoneRE.FindAllStringSubmatch(text, -1) {
		out = append(out, HookRun{Stamp: m[1] + " " + m[2], Exit: stringToNumber(m[3]), Reason: m[4]})
	}
	return out
}

const actionsStamp = `(?:\d{4}-\d{2}-\d{2}T[\d:.]+Z )?`

var (
	summaryRE = regexp.MustCompile(`^` + actionsStamp + `(\d+) blocking, (\d+) advisory \((work|world) scope: `)
	headerRE  = regexp.MustCompile(`^` + actionsStamp + `\[(BLOCKING|ADVISORY)\] ([a-z0-9][a-z0-9-]*) {2}`)
	invokeRE  = regexp.MustCompile(`\bnode\b[^\n;|&]*?\bcheck_the_(work|world)\.mjs\b`)
	ciLogRE   = regexp.MustCompile(`(` + ci("job") + `|` + ci("run") + `|` + ci("workflow") + `)_` + ci("logs"))
)

// Summary is one findings summary line.
type Summary struct {
	Scope              string
	Blocking, Advisory float64
}

// CheckSummaries are the findings summary lines a text carries, each at a
// line start.
func CheckSummaries(text string) []Summary {
	var out []Summary
	for _, m := range matchLines(summaryRE, text) {
		out = append(out, Summary{Scope: m[3], Blocking: stringToNumber(m[1]), Advisory: stringToNumber(m[2])})
	}
	return out
}

// Finding is one finding header.
type Finding struct {
	Severity, Rule string
}

// FindingHeaders are the finding headers a text carries, each at a line
// start.
func FindingHeaders(text string) []Finding {
	var out []Finding
	for _, m := range matchLines(headerRE, text) {
		sev := "advisory"
		if m[1] == "BLOCKING" {
			sev = "blocking"
		}
		out = append(out, Finding{Severity: sev, Rule: m[2]})
	}
	return out
}

// CheckInvocations are the runner invocations one Bash command names, per
// scope.
func CheckInvocations(command string) map[string]float64 {
	out := map[string]float64{"work": 0, "world": 0}
	for _, m := range invokeRE.FindAllStringSubmatch(command, -1) {
		out[m[1]]++
	}
	return out
}

// CheckOutput is one transcript text that can carry check output: a hook
// execution, a Bash result or a fetched CI job log.
type CheckOutput struct {
	Source, Text string
	Command      *string
}

// CheckOutputs reduces a capture's entries to the texts that can carry
// check output, a hook execution recorded twice and a CI log fetched twice
// once each.
func CheckOutputs(entries []any) []CheckOutput {
	type tool struct {
		source  string
		command *string
	}
	toolByID := map[string]tool{}
	idKey := func(v any, ok bool) string {
		if !ok {
			return "\x00undefined"
		}
		return Stringify(v)
	}
	for _, entry := range entries {
		if t, _ := pathString(entry, "type"); t != "assistant" {
			continue
		}
		blocks, _ := contentBlocks(entry)
		for _, b := range blocks {
			typ, _ := pathString(b, "type")
			name, isStr := pathString(b, "name")
			if typ != "tool_use" || !isStr {
				continue
			}
			id, idOK := path(b, "id")
			if _, isObj := id.(*Obj); isObj {
				continue
			}
			if cmd, ok := pathString(b, "input", "command"); name == "Bash" && ok {
				cmd := cmd
				toolByID[idKey(id, idOK)] = tool{source: "bash", command: &cmd}
			} else if ciLogRE.MatchString(name) {
				toolByID[idKey(id, idOK)] = tool{source: "ci"}
			}
		}
	}
	var out []CheckOutput
	seenHooks, seenCI := map[string]bool{}, map[string]bool{}
	hook := func(text string) {
		var stamps []string
		for _, r := range HookCheckRuns(text) {
			stamps = append(stamps, r.Stamp)
		}
		key := strings.Join(stamps, "|")
		if key != "" && seenHooks[key] {
			return
		}
		if key != "" {
			seenHooks[key] = true
		}
		out = append(out, CheckOutput{Source: "hook", Text: text})
	}
	for _, entry := range entries {
		typ, _ := pathString(entry, "type")
		sub, _ := pathString(entry, "subtype")
		hookEvent, _ := pathString(entry, "attachment", "hookEvent")
		isMeta, _ := path(entry, "isMeta")
		content, _ := path(entry, "message", "content")
		switch {
		case typ == "system" && sub == "stop_hook_summary":
			errs, _ := path(entry, "hookErrors")
			arr, _ := errs.([]any)
			var parts []string
			for _, e := range arr {
				parts = append(parts, joinElement(e))
			}
			hook(strings.Join(parts, "\n"))
		case typ == "attachment" && hookEvent == "Stop":
			stderr, _ := path(entry, "attachment", "stderr")
			stdout, _ := path(entry, "attachment", "stdout")
			hook(templateOrEmpty(stderr) + "\n" + templateOrEmpty(stdout))
		case typ == "user" && isMeta == true && isString(content):
			if strings.Contains(content.(string), "Stop hook feedback") {
				hook(content.(string))
			}
		case typ == "user":
			blocks, ok := content.([]any)
			if !ok {
				continue
			}
			for _, b := range blocks {
				if t, _ := pathString(b, "type"); t != "tool_result" {
					continue
				}
				useID, useOK := path(b, "tool_use_id")
				if _, isObj := useID.(*Obj); isObj {
					continue
				}
				tl, known := toolByID[idKey(useID, useOK)]
				if !known {
					continue
				}
				result, hasResult := path(entry, "toolUseResult")
				blockContent, _ := path(b, "content")
				text := resultText(result, hasResult, blockContent)
				if tl.source == "ci" {
					key, ok := checkOutputKey(text)
					if !ok || seenCI[key] {
						continue
					}
					seenCI[key] = true
				}
				out = append(out, CheckOutput{Source: tl.source, Text: text, Command: tl.command})
			}
		}
	}
	return out
}

func isString(v any) bool { _, ok := v.(string); return ok }

// joinElement is an element as Array#join writes it: null and undefined
// empty.
func joinElement(v any) string {
	if v == nil {
		return ""
	}
	return jsString(v)
}

// templateOrEmpty is `${v ?? ”}`.
func templateOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	return jsString(v)
}

// resultText is a tool result's text in whichever shape the tool reported
// it: a string, Bash's { stdout, stderr }, or the block's own content.
func resultText(result any, hasResult bool, blockContent any) string {
	if s, ok := result.(string); ok {
		return unwrapJSON(s)
	}
	if hasResult && truthy(result) {
		_, soOK := pathString(result, "stdout")
		_, seOK := pathString(result, "stderr")
		if soOK || seOK {
			stdout, _ := path(result, "stdout")
			stderr, _ := path(result, "stderr")
			return unwrapJSON(templateOrEmpty(stdout)) + "\n" + templateOrEmpty(stderr)
		}
	}
	if s, ok := blockContent.(string); ok {
		return unwrapJSON(s)
	}
	if arr, ok := blockContent.([]any); ok {
		parts := make([]string, len(arr))
		for i, b := range arr {
			if t, ok := pathString(b, "text"); ok {
				parts[i] = t
			}
		}
		return unwrapJSON(strings.Join(parts, "\n"))
	}
	if !hasResult || result == nil {
		return ""
	}
	return unwrapJSON(Stringify(result))
}

// unwrapJSON is a JSON document's every string leaf, joined with
// newlines; any other text as it stands.
func unwrapJSON(text string) string {
	trimmed := jsregex.Trim(text)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return text
	}
	parsed, err := ParseJSON(trimmed)
	if err != nil {
		return text
	}
	return strings.Join(EntryText(parsed), "\n")
}

func checkOutputKey(text string) (string, bool) {
	var marks []string
	for _, m := range matchLines(summaryRE, text) {
		marks = append(marks, m[0])
	}
	for _, m := range matchLines(headerRE, text) {
		marks = append(marks, m[0])
	}
	if len(marks) == 0 {
		return "", false
	}
	return strings.Join(marks, "|"), true
}

// Checks are one capture's check activations: per scope, and per rule.
type Checks struct {
	Checks, CheckFindings *Obj
}

// CountChecks counts one capture file's check activations. Runs come from
// the marks a passing run also leaves; a summary line counts a failure,
// and stands in for a run its command did not name.
func CountChecks(entries []any) Checks {
	checks, findings := NewObj(), NewObj()
	scope := func(name string) *Obj {
		row := checks.ObjAt(name)
		if row == nil {
			row = zeros(UsageFields["checks"])
			checks.Set(name, row)
		}
		return row
	}
	finding := func(rule string) *Obj {
		row := findings.ObjAt(rule)
		if row == nil {
			row = zeros(UsageFields["checkFindings"])
			findings.Set(rule, row)
		}
		return row
	}
	var blocks [][]Finding
	for _, o := range CheckOutputs(entries) {
		summaries := CheckSummaries(o.Text)
		switch o.Source {
		case "hook":
			if h := FindingHeaders(o.Text); len(h) > 0 {
				blocks = append(blocks, h)
			}
			for _, run := range HookCheckRuns(o.Text) {
				work := scope("work")
				bump(work, "runs", 1)
				if run.Reason == "runner-error" {
					bump(work, "errors", 1)
				}
				if run.Reason == "loop-guard-relent" {
					bump(work, "failures", 1)
					seen := map[string]bool{}
					if n := len(blocks); n > 0 {
						for _, f := range blocks[n-1] {
							if !seen[f.Rule] {
								seen[f.Rule] = true
								bump(finding(f.Rule), "relent", 1)
							}
						}
					}
				}
			}
			for _, s := range summaries {
				if s.Blocking > 0 {
					bump(scope(s.Scope), "failures", 1)
				}
			}
		case "ci":
			for _, name := range []string{"work", "world"} {
				var reported, failed float64
				for _, s := range summaries {
					if s.Scope == name {
						reported++
						if s.Blocking > 0 {
							failed++
						}
					}
				}
				if reported == 0 {
					continue
				}
				row := scope(name)
				bump(row, "runs", reported)
				bump(row, "ciRuns", reported)
				bump(row, "failures", failed)
				bump(row, "ciFailures", failed)
			}
		default:
			cmd := ""
			if o.Command != nil {
				cmd = *o.Command
			}
			invoked := CheckInvocations(cmd)
			for _, name := range []string{"work", "world"} {
				var reported, failed float64
				for _, s := range summaries {
					if s.Scope == name {
						reported++
						if s.Blocking > 0 {
							failed++
						}
					}
				}
				runs := math.Max(invoked[name], reported)
				if runs == 0 {
					continue
				}
				bump(scope(name), "runs", runs)
				bump(scope(name), "failures", failed)
			}
		}
		for _, s := range summaries {
			row := scope(s.Scope)
			bump(row, "blocking", s.Blocking)
			bump(row, "advisory", s.Advisory)
		}
		for _, f := range FindingHeaders(o.Text) {
			bump(finding(f.Rule), f.Severity, 1)
		}
	}
	for _, k := range findings.Keys() {
		findings.ObjAt(k).Set("sessions", 1.0)
	}
	if n := len(blocks); n > 0 {
		for _, f := range blocks[n-1] {
			if f.Severity == "advisory" {
				bump(finding(f.Rule), "persisted", 1)
			}
		}
	}
	return Checks{Checks: checks, CheckFindings: findings}
}

// CountTaskExecs are the executor's execution records one capture file
// attests, deduped on the whole record within the file.
func CountTaskExecs(entries []any) *Obj {
	seen := map[string]bool{}
	out := NewObj()
	for _, entry := range entries {
		for _, rec := range queue.ParseTaskExecs(strings.Join(EntryText(entry), "\n")) {
			key := rec.Pack + "/" + rec.Task + "|" + rec.Slot + "|" + rec.Status
			if seen[key] {
				continue
			}
			seen[key] = true
			id := rec.Pack + "/" + rec.Task
			row := out.ObjAt(id)
			if row == nil {
				row = zeros(queue.TaskExecStatuses)
				out.Set(id, row)
			}
			bump(row, rec.Status, 1)
		}
	}
	return out
}

func usageOf(entry any) (*Obj, bool) {
	if t, _ := pathString(entry, "type"); t != "assistant" {
		return nil, false
	}
	u, _ := path(entry, "message", "usage")
	switch x := u.(type) {
	case *Obj:
		return x, x != nil
	case []any:
		return NewObj(), true
	}
	return nil, false
}

func finiteOr0(o *Obj, k string) float64 {
	v, _ := o.Get(k)
	if isFiniteNumber(v) {
		return v.(float64)
	}
	return 0
}

// Tokens is a session's billed input (cache reads and writes included)
// and output.
type Tokens struct{ Input, Output float64 }

// TokensIn is what a capture's assistant entries were billed for; false
// where no entry records any usage.
func TokensIn(entries []any) (Tokens, bool) {
	var t Tokens
	seen := false
	for _, entry := range entries {
		u, ok := usageOf(entry)
		if !ok {
			continue
		}
		i := finiteOr0(u, "input_tokens") + finiteOr0(u, "cache_read_input_tokens") + finiteOr0(u, "cache_creation_input_tokens")
		o := finiteOr0(u, "output_tokens")
		if i == 0 && o == 0 {
			continue
		}
		seen = true
		t.Input += i
		t.Output += o
	}
	return t, seen
}

// TokensByModelUnknown keys a spend whose entry names no model.
const TokensByModelUnknown = "(unknown)"

// TokensByModelIn is the same spend per model, its four counters apart;
// nil where no entry records any usage.
func TokensByModelIn(entries []any) *Obj {
	out := NewObj()
	for _, entry := range entries {
		u, ok := usageOf(entry)
		if !ok {
			continue
		}
		vals := []float64{finiteOr0(u, "input_tokens"), finiteOr0(u, "cache_read_input_tokens"), finiteOr0(u, "cache_creation_input_tokens"), finiteOr0(u, "output_tokens")}
		if vals[0] == 0 && vals[1] == 0 && vals[2] == 0 && vals[3] == 0 {
			continue
		}
		model, _ := pathString(entry, "message", "model")
		if model == "" {
			model = TokensByModelUnknown
		}
		into := out.ObjAt(model)
		if into == nil {
			into = zeros(UsageFields["tokensByModel"])
			out.Set(model, into)
		}
		for i, f := range UsageFields["tokensByModel"] {
			bump(into, f, vals[i])
		}
	}
	if out.Len() == 0 {
		return nil
	}
	return out
}

// Seconds is how a session's wall clock divided between the person and
// the agent.
type Seconds struct{ Human, Agent float64 }

// TurnSeconds divides a capture's wall clock by who produced each entry:
// a human turn's gap before it is the person's, capped, an assistant
// entry's the agent's; sidechains are excluded. False where no entry
// carries a timestamp.
func TurnSeconds(entries []any, limit float64) (Seconds, bool) {
	type stamped struct {
		entry any
		at    float64
	}
	var list []stamped
	for _, e := range entries {
		if side, _ := path(e, "isSidechain"); side == true {
			continue
		}
		ts, ok := pathString(e, "timestamp")
		if !ok {
			continue
		}
		at := parseDate(ts)
		if math.IsNaN(at) {
			continue
		}
		list = append(list, stamped{e, at})
	}
	if len(list) == 0 {
		return Seconds{}, false
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].at-list[j].at < 0 })
	var human, agent float64
	var prev *float64
	for _, s := range list {
		if prev != nil {
			gap := math.Max(0, (s.at-*prev)/1000)
			if IsUserMessage(s.entry) {
				human += math.Min(limit, gap)
			} else if t, _ := pathString(s.entry, "type"); t == "assistant" {
				agent += gap
			}
		}
		at := s.at
		prev = &at
	}
	return Seconds{Human: jsRound(human), Agent: jsRound(agent)}, true
}

// The per-task cost split's two keys that are states rather than tasks:
// a session a person started, and one that names an issue or a pull
// request but attests no execution record.
const (
	TaskCostNone       = "(none)"
	TaskCostUnresolved = "(unresolved)"
)

// TaskCostKey is the task a capture's session belongs to; keyed is the
// number its filename carries.
func TaskCostKey(taskExec *Obj, keyed any) string {
	if named := sortedKeys(taskExec); len(named) > 0 {
		return named[0]
	}
	if toNumber(keyed) > 0 {
		return TaskCostUnresolved
	}
	return TaskCostNone
}

// TaskCostRank is what a key is worth when a session's captures disagree.
func TaskCostRank(key *string) int {
	switch {
	case key == nil:
		return 0
	case *key == TaskCostNone:
		return 1
	case *key == TaskCostUnresolved:
		return 2
	}
	return 3
}

// Corpus is what the mounted corpus offers the counters that need it:
// the mounted skill names, and the engine's force-load declarations, its
// predicates and the rule-to-skill ownership, which the fold is never
// handed, so the counters reading them record no key.
type Corpus struct {
	Mounted      map[string]bool
	Declarations []Declaration
	Hits         Hits
	OwnerOf      func(rule string) string
}

// Counts are one capture file's counters.
type Counts struct {
	UserMessages, UserCommands float64
	SkillLoads                 *Obj
	Tokens                     *Tokens
	TokensByModel              *Obj
	Seconds                    *Seconds
	TaskExec                   *Obj
	Checks, CheckFindings      *Obj
	SkillLoadsBy, SkillBlocks  *Obj
	TriggerFires, GuardFires   *Obj
	ToolCalls                  *Obj
	Moments, CheckTiming       *Obj
	CheckBuild                 *CheckBuild
	SkillCaught                *Obj
}

// group is a counter group or bare map by name, nil for one a capture
// does not count.
func (c Counts) group(name string) *Obj {
	switch name {
	case "skillLoads":
		return c.SkillLoads
	case "skillBlocks":
		return c.SkillBlocks
	case "moments":
		return c.Moments
	case "toolCalls":
		return c.ToolCalls
	case "skillCaught":
		return c.SkillCaught
	case "checks":
		return c.Checks
	case "checkFindings":
		return c.CheckFindings
	case "taskExec":
		return c.TaskExec
	case "skillLoadsBy":
		return c.SkillLoadsBy
	case "triggerFires":
		return c.TriggerFires
	case "guardFires":
		return c.GuardFires
	case "checkTiming":
		return c.CheckTiming
	}
	return nil
}

// CountEntries counts one capture file. A typed /command counts as a
// skill load only when it names a mounted skill.
func CountEntries(entries []any, corpus Corpus) Counts {
	mounted := corpus.Mounted
	if mounted == nil {
		mounted = map[string]bool{}
	}
	c := Counts{SkillLoads: NewObj()}
	for _, entry := range entries {
		for _, name := range SkillToolLoads(entry) {
			bumpOr(c.SkillLoads, name, 1.0)
		}
		if IsUserMessage(entry) {
			c.UserMessages++
		}
		if command, ok := CommandName(entry); ok {
			c.UserCommands++
			if mounted[command] {
				bumpOr(c.SkillLoads, command, 1.0)
			}
		}
	}
	if t, ok := TokensIn(entries); ok {
		c.Tokens = &t
	}
	c.TokensByModel = TokensByModelIn(entries)
	if s, ok := TurnSeconds(entries, HumanSecondsCap); ok {
		c.Seconds = &s
	}
	c.TaskExec = CountTaskExecs(entries)
	checks := CountChecks(entries)
	c.Checks, c.CheckFindings = checks.Checks, checks.CheckFindings
	use := CountCorpusUse(entries, mounted)
	c.SkillLoadsBy, c.SkillBlocks, c.TriggerFires, c.GuardFires, c.ToolCalls = use.SkillLoadsBy, use.SkillBlocks, use.TriggerFires, use.GuardFires, use.ToolCalls
	c.Moments = CountMoments(entries, corpus.Declarations, corpus.Hits)
	c.CheckTiming = CountCheckTiming(entries)
	c.CheckBuild = ReadCheckBuild(entries)
	c.SkillCaught = CaughtSkills(use.SkillLoadsBy.Keys(), c.CheckFindings, corpus.OwnerOf)
	return c
}

// CaughtSkills are the skills that loaded and were caught anyway by a
// check they own; nothing without the ownership.
func CaughtSkills(loaded []string, findings *Obj, ownerOf func(string) string) *Obj {
	out := NewObj()
	if ownerOf == nil {
		return out
	}
	held := map[string]bool{}
	for _, s := range loaded {
		held[s] = true
	}
	for _, rule := range findings.Keys() {
		b, _ := findings.ObjAt(rule).Get("blocking")
		if !(toNumber(b) > 0) {
			continue
		}
		if skill := ownerOf(rule); skill != "" && held[skill] {
			out.Set(skill, 1.0)
		}
	}
	return out
}
