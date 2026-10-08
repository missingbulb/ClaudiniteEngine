// Package taskspec is the task declaration contract: what a
// tasks/<name>/task.json (or .yaml, .toml) must carry, its normalisation,
// its validation and its discovery across the declared packs plus the
// engine's own built-in root. The runtime and the author-time check
// validate against the one function here, so the accepted shape cannot
// drift between them. Rules and messages are string-identical with the
// Node engine's task-contract.mjs at missingbulb/Claudinite@057841ac; the
// precondition grammar's static half lives here too, and tasks/precondition
// holds how each term judges.
package taskspec

import (
	"math"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsregex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
)

// Problem is one way a declaration is not well formed.
type Problem struct {
	What string `json:"what"`
	Fix  string `json:"fix"`
}

// The triggers: who mints an occurrence.
const (
	TriggerSchedule = "schedule"
	TriggerRequest  = "request"
)

// Triggers are the legal triggers.
var Triggers = []string{TriggerSchedule, TriggerRequest}

// Outcomes are the write ceilings: what a run does to pull requests.
var Outcomes = []string{"no_code_changes", "fresh_pr", "amend_existing_or_create_new_pr", "supersede_existing_pr"}

// OutcomeNoPR opens no pull request.
const OutcomeNoPR = "no_code_changes"

// ModelFamilies are the families a task names; none runs no agent.
var ModelFamilies = []string{"opus", "sonnet", "haiku", "none"}

// InterruptPolicies are what a recovery path does with an interrupted
// item.
var InterruptPolicies = []string{"requeue", "needs-human"}

// SignalNames are the signal collectors.
var SignalNames = []string{"commits", "prs", "issues", "branches", "release", "localPacks", "sharedMount", "conversationLogs", "stamp", "fleet", "request"}

// DescriptionMaxWords bounds a description to a summary.
const DescriptionMaxWords = 50

// Decl is a declaration as parsed: JSON-shaped values by key.
type Decl map[string]any

// Str is a string field, and whether it is one.
func (d Decl) Str(key string) (string, bool) {
	s, ok := d[key].(string)
	return s, ok
}

// Has reports whether the field is declared at all.
func (d Decl) Has(key string) bool {
	_, ok := d[key]
	return ok
}

// Strings is a field's string list, nil when it is not one.
func (d Decl) Strings(key string) []string {
	list, ok := d[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, isStr := e.(string)
		if !isStr {
			return nil
		}
		out = append(out, s)
	}
	return out
}

// Num is a number field.
func (d Decl) Num(key string) (float64, bool) {
	f, ok := d[key].(float64)
	return f, ok
}

// ID is the task's id.
func (d Decl) ID() string { s, _ := d.Str("id"); return s }

// AgentModel is the declared (or defaulted) family.
func (d Decl) AgentModel() string {
	if s, ok := d.Str("agent_model"); ok {
		return s
	}
	return workitem.DefaultAgentModel
}

// Outcome is the declared ceiling.
func (d Decl) Outcome() string { s, _ := d.Str("expected_outcome"); return s }

// Preconditions is the expression, as declared.
func (d Decl) Preconditions() any { return d["preconditions"] }

// DeclaresCodeWork reports whether the task has a deterministic work step
// in either of its forms.
func (d Decl) DeclaresCodeWork() bool { return d.Has("code_work") || d.Has("code_worker_mjs") }

// IsScheduled reports whether the scheduler asks the task at every tick.
func (d Decl) IsScheduled() bool { return d["trigger"] == TriggerSchedule }

// Cadence is the cadence the task keeps, or nil.
func (d Decl) Cadence() *Cadence { return CadenceOf(d["preconditions"]) }

// RunsAgent reports whether the task has an agentic phase.
func (d Decl) RunsAgent() bool { return d.AgentModel() != "none" }

// OpensPullRequest reports whether a canonical outcome lets the run open a
// pull request.
func OpensPullRequest(outcome string) bool { return outcome != OutcomeNoPR }

// CanonicalOutcome is the outcome when it is a ceiling, or "".
func CanonicalOutcome(outcome string) string {
	if has(Outcomes, outcome) {
		return outcome
	}
	return ""
}

// Normalize is the declaration with canonical spellings and the defaults
// filled in: $schema dropped, due:<cadence> rewritten, absent
// preconditions as the empty expression, agent_model none, and automerge
// nothing beside an outcome that may open a pull request. A value that is
// not an object passes through for Validate to report.
func Normalize(raw any) any {
	obj, ok := asObject(raw)
	if !ok {
		return raw
	}
	out := Decl{}
	for k, v := range obj {
		out[k] = v
	}
	delete(out, "$schema")
	if v, present := out["preconditions"]; present {
		out["preconditions"] = NormalizeCadenceTerms(v)
	} else {
		out["preconditions"] = []any{}
	}
	if !out.Has("agent_model") {
		out["agent_model"] = workitem.DefaultAgentModel
	}
	if o, present := out["expected_outcome"]; present && o != OutcomeNoPR && !out.Has("automerge") {
		out["automerge"] = workitem.DefaultAutomerge
	}
	return out
}

func asObject(v any) (Decl, bool) {
	switch x := v.(type) {
	case Decl:
		return x, true
	case map[string]any:
		return Decl(x), true
	}
	return nil, false
}

func wordCount(text string) int { return len(strings.FieldsFunc(text, jsregex.IsSpace)) }

// DescriptionProblem judges a declared description.
func DescriptionProblem(v any) *Problem {
	s, ok := v.(string)
	switch {
	case !ok:
		return &Problem{`"description" is not a string`, "write one sentence or two saying what the task does or why it exists"}
	case jsregex.Trim(s) == "":
		return &Problem{`"description" is empty`, "say what the task does or why it exists, or drop the field"}
	}
	if n := wordCount(s); n > DescriptionMaxWords {
		return &Problem{`"description" runs to ` + itoa(n) + " words", "keep it to " + itoa(DescriptionMaxWords) + " words — a summary, not the README"}
	}
	return nil
}

func itoa(n int) string { return jsjson.FormatNumber(float64(n)) }

// stringify is JSON.stringify of a field, "undefined" when absent.
func stringify(d Decl, key string) string {
	v, ok := d[key]
	if !ok {
		return "undefined"
	}
	return jsjson.StringifyAny(v)
}

// str is String() of a field, "undefined" when absent.
func str(d Decl, key string) string {
	v, ok := d[key]
	if !ok {
		return "undefined"
	}
	return jsjson.StringOf(v)
}

func isPositiveInt(v any) bool {
	f, ok := v.(float64)
	return ok && f > 0 && f == math.Trunc(f) && !math.IsInf(f, 0)
}

func escapesTaskDir(cmd string) bool {
	if strings.Contains(cmd, "..") || strings.HasPrefix(cmd, "/") {
		return true
	}
	prev := rune(0)
	for _, r := range cmd {
		if r == '/' && jsregex.IsSpace(prev) {
			return true
		}
		prev = r
	}
	return false
}

func hasSpace(s string) bool { return strings.IndexFunc(s, jsregex.IsSpace) >= 0 }

func isTaskRef(s string) bool {
	pack, task, ok := strings.Cut(s, "/")
	return ok && pack != "" && task != "" && !strings.Contains(task, "/") && !hasSpace(s)
}

func isKebab(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, "-") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return false
			}
		}
	}
	return true
}

func quoted(list []string) string {
	q := make([]string, len(list))
	for i, s := range list {
		q[i] = `"` + s + `"`
	}
	return strings.Join(q, ", ")
}

// Validate judges one declaration, normalising it first. An empty list
// means the declaration is well formed. terms are the task's own
// precondition terms.
func Validate(raw any, terms Terms) []Problem {
	n := Normalize(raw)
	d, ok := asObject(n)
	if !ok {
		return []Problem{{"task.json is not a declaration object", `write one JSON object: { "id", "description", "expected_outcome", … }`}}
	}
	problems := []Problem{}
	bad := func(what, fix string) { problems = append(problems, Problem{what, fix}) }

	if id, ok := d.Str("id"); !ok || jsregex.Trim(id) == "" {
		bad(`the task has no string "id"`, `give the task an "id" matching its directory name`)
	}
	if v, present := d["description"]; present {
		if p := DescriptionProblem(v); p != nil {
			bad(p.What, p.Fix)
		}
	}
	if d.Has("precondition_signals") {
		bad(`"precondition_signals" is retired`, "drop it — the signal union is derived from the conditions, each of which names what it reads")
	}
	model, modelIsStr := d.Str("agent_model")
	if !modelIsStr || !has(ModelFamilies, model) {
		bad(`"agent_model" `+stringify(d, "agent_model")+" is not a legal model family", "set one of: "+strings.Join(ModelFamilies, ", "))
	}
	outcome, outcomeIsStr := d.Str("expected_outcome")
	if !outcomeIsStr || !has(Outcomes, outcome) {
		bad(`"expected_outcome" `+stringify(d, "expected_outcome")+" is not a legal outcome ceiling", "set one of: "+strings.Join(Outcomes, ", "))
	}
	if !outcomeIsStr || outcome != OutcomeNoPR {
		if !d.Has("automerge") {
			bad(`a "`+str(d, "expected_outcome")+`" task declares no "automerge"`, `say what may land unreviewed: "nothing", "anything", or a list of diff classes, e.g. ["comment-only-changes", "readme-changes"]`)
		} else if p := mergepolicy.Normalize(d["automerge"]); p.Kind == "invalid" {
			bad(`"automerge" is not a legal policy: `+p.Reason, `set "nothing", "anything", or a list of rule names, each optionally reject:-prefixed`)
		}
	} else if d.Has("automerge") {
		bad(`a "`+OutcomeNoPR+`" task declares "automerge"`, `drop it — a task that opens no pull request has nothing to merge; or set expected_outcome: "fresh_pr"`)
	}
	if !modelIsStr || model != "none" {
		if s, ok := d.Str("agent_instructions"); !ok || jsregex.Trim(s) == "" {
			bad(`an agentic task (agent_model !== "none") declares no string "agent_instructions"`, `point "agent_instructions" at the worker file beside task.json (e.g. "task.md")`)
		}
	}
	triggerFix := "write one of: " + quoted(Triggers) + ` — "` + TriggerSchedule + `" is asked by the scheduler at every tick, "` + TriggerRequest + `" runs only from an item somebody creates`
	if !d.Has("trigger") {
		bad(`the task declares no "trigger"`, triggerFix)
	} else if t, ok := d.Str("trigger"); !ok || !has(Triggers, t) {
		bad(`"`+str(d, "trigger")+`" is not a legal trigger`, triggerFix)
	}
	if d.Has("precondition") {
		bad(`the task declares a "precondition" function, which is retired`, `move the gate into "preconditions" — a built-in condition, or a term this task's preconditions.mjs exports`)
	}
	if d.Has("frequency") {
		f, isStr := d.Str("frequency")
		term := ScheduleTermFor("<" + strings.Join(Cadences, "|") + ">")
		if isStr && has(Frequencies, f) {
			term = cadenceTermFor(f)
		}
		if term == "" {
			bad(`the task declares "frequency", which is retired`, `drop it and write "trigger": "request" - "manual" meant no schedule at all, which a declaration now says outright`)
		} else {
			bad(`the task declares "frequency", which is retired`, `write the cadence as a condition - "preconditions": ["`+term+`", …] - with "trigger": "schedule" beside it, and drop a "none"`)
		}
	}
	if d.Has("preconditions") {
		problems = append(problems, ValidatePreconditions(d["preconditions"], terms)...)
	}
	if v, present := d["model_from_request"]; present && v != true {
		bad(`"model_from_request" `+jsjson.StringifyAny(v)+" is not `true`", "drop the field — only the engine's built-in request task reads a model off its item, and every other task names its own agent_model")
	}
	if d.Has("code_work") && d.Has("code_worker_mjs") {
		bad(`both "code_work" and "code_worker_mjs" are declared`, `keep one - "code_worker_mjs" for a module the runner wraps, "code_work" for a command it only spawns`)
	}
	if d.Has("code_work") {
		if cmd, ok := d.Str("code_work"); !ok || jsregex.Trim(cmd) == "" {
			bad(`"code_work" is present but not a non-empty string`, `set it to a command whose executable is a script beside task.json, e.g. "node prepare.mjs"`)
		} else if escapesTaskDir(cmd) {
			bad(`"code_work" reaches outside the task directory (absolute path or "..")`, `reference a sibling script only, e.g. "node prepare.mjs"`)
		}
	}
	if d.Has("code_worker_mjs") {
		mod, ok := d.Str("code_worker_mjs")
		switch {
		case !ok || jsregex.Trim(mod) == "":
			bad(`"code_worker_mjs" is present but not a non-empty string`, "name the module beside task.json that exports `worker`, e.g. \"worker.mjs\"")
		case hasSpace(jsregex.Trim(mod)):
			bad(`"code_worker_mjs" is a command rather than a file name`, `name the module alone, e.g. "worker.mjs" - the runner supplies the node invocation`)
		case !strings.HasSuffix(mod, ".mjs"):
			bad(`"code_worker_mjs" does not name a .mjs module`, "the runner imports it and calls its `worker` export, so it is an ES module beside task.json")
		case escapesTaskDir(mod):
			bad(`"code_worker_mjs" reaches outside the task directory (absolute path or "..")`, `name a sibling module only, e.g. "worker.mjs"`)
		}
	}
	if d.DeclaresCodeWork() {
		field := "code_work"
		if d.Has("code_worker_mjs") {
			field = "code_worker_mjs"
		}
		leash := int(workitem.ExecutingLeash.Minutes())
		if !isPositiveInt(d["code_work_timeout"]) {
			bad(`"`+field+`" is set but "code_work_timeout" is not a positive integer`, `add "code_work_timeout": the seconds after which the subprocess is killed and the task fails`)
		} else if t, _ := d.Num("code_work_timeout"); t*1000 >= float64(workitem.ExecutingLeash.Milliseconds()) {
			bad(`"code_work_timeout" (`+jsjson.FormatNumber(t)+"s) reaches the executor's "+itoa(leash)+"-minute claim leash",
				"bound the work step under "+itoa(leash)+" minutes - one that can outlive the leash is reclaimed while still running, and the item livelocks")
		}
	}
	if v, present := d["schedule_after"]; present {
		ok := false
		if list, isList := v.([]any); isList {
			ok = true
			for _, e := range list {
				if s, isStr := e.(string); !isStr || !isTaskRef(s) {
					ok = false
				}
			}
		}
		if !ok {
			bad(`"schedule_after" is not an array of "<pack>/<task>" ids`, `e.g. "schedule_after": ["claudinite-lifecycle/update"] — this task is not scheduled onto an executor while those are live this cycle`)
		}
	}
	if v, present := d["on_interrupt"]; present {
		if s, ok := v.(string); !ok || !has(InterruptPolicies, s) {
			bad(`"on_interrupt" `+jsjson.StringifyAny(v)+" is not a legal policy", "set one of: "+strings.Join(InterruptPolicies, ", ")+` (default "requeue")`)
		}
	}
	if v, present := d["invocation_endpoint"]; present {
		if s, ok := v.(string); !ok || !isKebab(s) {
			bad(`"invocation_endpoint" is not a kebab-case endpoint name`, `name a key from the repo's taskScheduler.agenticTaskInvocationEndpoints map, e.g. "default" — never a URL`)
		}
	}
	secrets, secretsAreList := d["code_work_required_secrets"].([]any)
	if d.Has("code_work_required_secrets") {
		ok := secretsAreList
		for _, e := range secrets {
			if s, isStr := e.(string); !isStr || jsregex.Trim(s) == "" {
				ok = false
			}
		}
		if !ok {
			bad(`"code_work_required_secrets" is not an array of secret names`, `list the repo Actions secret names this task needs, e.g. ["SOME_API_KEY"]`)
		}
	}
	for _, e := range secrets {
		name, isStr := e.(string)
		if !isStr {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(name), "GITHUB_") {
			bad(`required secret "`+name+`" cannot be created — GitHub reserves the GITHUB_ prefix`, "rename it without that prefix, e.g. one carrying the pack's own name")
		}
		if strings.HasPrefix(strings.ToUpper(name), "CLAUDINITE_") {
			bad(`required secret "`+name+`" sits in the code-work namespace, which its task's own code may not read`, "rename it outside CLAUDINITE_* — that prefix belongs to the variables code_work is handed")
		}
	}
	if modelIsStr && has(ModelFamilies, model) && model != "none" && !isPositiveInt(d["agent_execution_timeout"]) {
		bad(`an agentic task (agent_model !== "none") declares no positive-integer "agent_execution_timeout"`, `add "agent_execution_timeout": the seconds bounding the agentic run — generous; extreme protection, not a scheduling knob`)
	}
	if modelIsStr && model == "none" && !d.DeclaresCodeWork() {
		bad(`an agentless task (agent_model: "none") declares no work step`, `add "code_worker_mjs" (a none task does its work in that subprocess) - or give the task an agent_model`)
	}
	return problems
}
