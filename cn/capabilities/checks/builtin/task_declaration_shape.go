package builtin

import (
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
)

// Every tasks/<name>/task.* carries the task contract with legal values,
// asserted at author time; the runtime validates the same contract, so an
// illegal or missing value is caught here first. Inert on a repo with no
// task declaration. A task-local term is read off the sibling
// preconditions.mjs as text, never imported: a check must not execute a
// member's own module.
var taskDeclarationShape = declared.Builtin{
	ID:     "task-declaration-shape",
	Pack:   declared.EnginePack,
	OnFail: "block",
	Tags:   []string{"world", "builtin", declared.EnginePack},
	Doc:    "docs/tasks-principles.md",
	Why:    "the scheduler run and executor read agent_model/expected_outcome/preconditions from this file, not the work item — an illegal or missing value means a task never fires, fires wrong, or writes past its ceiling",
}

func init() { register(&taskDeclarationShape, runTaskDeclarationShape) }

// taskDeclarationPath selects a task declaration in any format; group 2 is
// the task's directory name.
var taskDeclarationPath = regexp.MustCompile(`(^|/)tasks/([^/]+)/task\.(json|yaml|toml)$`)

func siblingTerms(ctx *declared.Ctx, taskFile string) taskspec.Terms {
	text, ok := ctx.Read(taskFile[:strings.LastIndex(taskFile, "/")+1] + "preconditions.mjs")
	if !ok {
		return nil
	}
	return taskspec.TermsFromText(checksdk.StripComments(text))
}

func runTaskDeclarationShape(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	var out []findings.Finding
	b := taskDeclarationShape
	for _, file := range ctx.Files() {
		if !taskDeclarationPath.MatchString(file) {
			continue
		}
		text, ok := ctx.Read(file)
		if !ok {
			continue
		}
		flag := func(what, fix string) { out = append(out, b.Finding(file, 0, what, fix)) }
		advise := func(what, fix string) { out = append(out, b.Advice(file, 0, what, fix)) }

		d, err := taskspec.ParseText(file, []byte(text))
		if err != nil {
			flag("is not a JSON object: "+err.Error(), `write one JSON object: { "id", "description", "preconditions", "expected_outcome", … }`)
			continue
		}
		str := func(key string) (string, bool) { return d.Str(key) }
		scalar := func(key string) any { return scalarOrNil(d[key]) }
		hasNum := func(key string) bool { _, ok := d.Num(key); return ok }
		// list is a field's string list: absent, declared but not one, or
		// the list (an empty one included).
		list := func(key string) (pre []string, declared bool) {
			return d.Strings(key), d.Has(key)
		}
		terms := siblingTerms(ctx, file)

		if d.Has("frequency") {
			declaredFreq, _ := str("frequency")
			term := taskspec.ScheduleTermFor("<" + strings.Join(taskspec.Cadences, "|") + ">")
			if inList(taskspec.Frequencies, declaredFreq) {
				term = taskspec.CadenceTermFor(declaredFreq)
			}
			if term == "" {
				flag(`declares "frequency", which is retired`, `drop the field, and a "none" beside it, and write "trigger": "request" - "manual" meant no schedule at all`)
			} else {
				flag(`declares "frequency", which is retired`, `write the cadence as a condition - "preconditions": [`+jsjson.StringifyAny(term)+`, …] - with "trigger": "schedule" beside it, and drop a "none"`)
			}
		}

		pre, _ := list("preconditions")
		if !d.Has("trigger") {
			flag(`declares no "trigger"`, `add "trigger": one of `+strings.Join(taskspec.Triggers, ", ")+`. "`+taskspec.TriggerSchedule+`" is asked by the scheduler at every tick, "`+taskspec.TriggerRequest+`" runs only from an item somebody creates`)
		} else {
			trigger, isStr := str("trigger")
			switch {
			case !isStr || !inList(taskspec.Triggers, trigger):
				flag(`"trigger" is `+jsjson.StringifyAny(scalar("trigger"))+", not a legal value",
					"use one of: "+strings.Join(taskspec.Triggers, ", ")+` — "`+taskspec.TriggerSchedule+`" is asked by the scheduler at every tick, "`+taskspec.TriggerRequest+`" runs only from an item somebody creates`)
			case trigger == taskspec.TriggerSchedule && pre != nil && taskspec.NeedsItem(toAny(pre), terms):
				flag(`a "schedule" task states a condition that reads the item itself`,
					`write "trigger": "`+taskspec.TriggerRequest+`" — a condition about one item can only be judged once an item exists, and the scheduler's ask at a tick has none, so this task would fail every tick instead of declining`)
			case trigger == taskspec.TriggerRequest && pre != nil && taskspec.CadenceOf(toAny(pre)) != nil:
				flag(`a "request" task states a cadence term`,
					"drop the term — nothing asks this task, so every item of it is one somebody created and carries `Woken:`, which satisfies a cadence; the term can never decline a run, it only reads as though it limits the lever")
			}
		}

		declaredModel, modelIsStr := str("agent_model")
		if d.Has("agent_model") && (!modelIsStr || !inList(taskspec.ModelFamilies, declaredModel)) {
			flag(`"agent_model" is `+jsjson.StringifyAny(scalar("agent_model"))+", not a legal value", "use one of: "+strings.Join(taskspec.ModelFamilies, ", ")+" — or drop it: a task with no agent_model runs no agent")
		}
		model := workitem.DefaultAgentModel
		if modelIsStr {
			model = declaredModel
		}

		outcome, outcomeIsStr := str("expected_outcome")
		if !outcomeIsStr {
			flag(`declares no "expected_outcome"`, `add "expected_outcome": one of `+strings.Join(taskspec.Outcomes, ", "))
		} else if !inList(taskspec.Outcomes, outcome) {
			flag(`"expected_outcome" is "`+outcome+`", not a legal value`, "use one of: "+strings.Join(taskspec.Outcomes, ", "))
		}
		if outcomeIsStr && outcome == taskspec.OutcomeNoPR && d.Has("automerge") {
			flag(`a "`+taskspec.OutcomeNoPR+`" task declares "automerge"`, `drop it — a task that opens no pull request has nothing to merge; or set expected_outcome: "fresh_pr"`)
		}

		if _, ok := str("id"); !ok {
			flag(`declares no string "id"`, `add "id": the task name (matching its directory)`)
		}
		// Advisory when absent, so a converted task's vendor refresh does
		// not go red over it; blocking when declared badly.
		if !d.Has("description") {
			advise(`declares no "description"`, `add "description": up to fifty words on what the task does or why it exists — not what the other fields already say`)
		} else if p := taskspec.DescriptionProblem(scalar("description")); p != nil {
			flag(p.What, p.Fix)
		}
		if _, ok := str("agent_instructions"); model != "none" && !ok {
			flag(`an agentic task (agent_model !== "none") declares no string "agent_instructions"`, `add "agent_instructions": the worker file beside the declaration (e.g. "task.md")`)
		}
		if d.Has("precondition") {
			flag(`declares "precondition", which is retired`, `move the gate into "preconditions" — a built-in condition, or a term this task's preconditions.mjs exports`)
		}
		if d.Has("precondition_signals") {
			flag(`declares "precondition_signals", which is retired`, "drop it — the signal union is derived from the conditions, each of which names what it reads")
		}
		if stated, declared := list("preconditions"); declared {
			if stated == nil {
				flag(`"preconditions" is not a literal list of condition strings`, `write it as a literal, e.g. "preconditions": ["due:daily", "substantive-change"] — a computed expression is unreadable to this check and to the next person`)
			} else {
				expression := taskspec.NormalizeCadenceTerms(toAny(stated))
				for _, p := range taskspec.ValidatePreconditions(expression, terms) {
					flag(p.What, p.Fix)
				}
			}
		}

		codeWork, hasCodeWork := str("code_work")
		worker, hasWorker := str("code_worker_mjs")
		anyWork := hasCodeWork || hasWorker
		if model != "none" && !hasNum("agent_execution_timeout") {
			flag(`an agentic task (agent_model !== "none") declares no numeric "agent_execution_timeout"`, `add "agent_execution_timeout": seconds bounding the agentic run`)
		}
		if model == "none" && !anyWork {
			flag(`an agentless task (agent_model: "none") declares no work step`, `add "code_worker_mjs" (a none task does its work in that subprocess) - or give the task an agent_model`)
		}
		if hasCodeWork && hasWorker {
			flag(`both "code_work" and "code_worker_mjs" are declared`, `keep one - "code_worker_mjs" for a module the runner wraps, "code_work" for a command it only spawns`)
		}
		if hasWorker {
			switch {
			case taskspec.HasSpace(worker):
				flag(`"code_worker_mjs" is a command rather than a file name`, `name the module alone, e.g. "worker.mjs" - the runner supplies the node invocation`)
			case !strings.HasSuffix(worker, ".mjs"):
				flag(`"code_worker_mjs" does not name a .mjs module`, "the runner imports it and calls its `worker` export, so it is an ES module beside task.json")
			case taskspec.EscapesTaskDir(worker):
				flag(`"code_worker_mjs" reaches outside the task directory (absolute path or "..")`, `name a sibling module only, e.g. "worker.mjs"`)
			}
		}
		if anyWork {
			if codeWork != "" && taskspec.EscapesTaskDir(codeWork) {
				flag(`"code_work" reaches outside the task directory (absolute path or "..")`, `reference a sibling script only, e.g. "node prepare.mjs"`)
			}
			if !hasNum("code_work_timeout") {
				flag(`a work step is declared but no numeric "code_work_timeout" is`, `add "code_work_timeout": seconds after which the subprocess is killed`)
			}
		}
	}
	return out
}

func toAny(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

func inList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func scalarOrNil(v any) any {
	switch v.(type) {
	case string, float64, bool:
		return v
	}
	return nil
}
