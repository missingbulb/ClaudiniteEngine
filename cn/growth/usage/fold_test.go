package usage

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// legacyExecutorDoc is the executor doc a usage row from before the task
// surface moved still names: data a decoder keeps reading.
const legacyExecutorDoc = "engine/scheduler/executor.md"

// lit is a JSON literal as the value model holds it.
func lit(text string) any {
	v, err := ParseJSON(text)
	if err != nil {
		panic(text + ": " + err.Error())
	}
	return v
}

func litObj(text string) *Obj { return lit(text).(*Obj) }

// emptyGroups is every sub-map a row carries, all empty, derived from the
// vocabulary.
func emptyGroups() *Obj {
	o := NewObj()
	for _, m := range BareMaps {
		o.Set(m, NewObj())
	}
	for _, g := range CounterGroups {
		o.Set(g, NewObj())
	}
	return o
}

// blankDay is a day row as FoldDays builds an empty one.
func blankDay() *Obj { return spread(zeros(CaptureDayFields), emptyGroups()) }

// findingRow is one rule's findings row with every slot present.
func findingRow(over string) *Obj { return spread(zeros(UsageFields["checkFindings"]), litObj(over)) }

// --- entry fixtures, copied from real captured transcripts ----------------

func humanSaid(text string) *Obj {
	return ObjOf("type", "user", "origin", ObjOf("kind", "human"), "promptSource", "sdk", "userType", "external",
		"message", ObjOf("content", text))
}

func toolResultTurn() *Obj {
	return litObj(`{"type": "user", "message": {"content": [{"type": "tool_result", "content": "ok"}]}}`)
}

func metaTurn(text string) *Obj {
	return ObjOf("type", "user", "isMeta", true, "message", ObjOf("content", text))
}

func sidechainTurn(text string) *Obj {
	return ObjOf("type", "user", "isSidechain", true, "message", ObjOf("content", text))
}

func scheduledTurn(text string) *Obj {
	return ObjOf("type", "user", "origin", ObjOf("kind", "task-notification", "subkind", "scheduled-trigger"),
		"message", ObjOf("content", text))
}

func notifiedTurn(text string) *Obj {
	return ObjOf("type", "user", "origin", ObjOf("kind", "task-notification"), "message", ObjOf("content", text))
}

func compactTurn() *Obj {
	return ObjOf("type", "user", "isCompactSummary", true, "isVisibleInTranscriptOnly", true,
		"message", ObjOf("content", "This session is being continued from a previous conversation…"))
}

func slash(name, args string) *Obj {
	return ObjOf("type", "user", "message", ObjOf("content",
		"<command-name>/"+name+"</command-name>\n<command-message>"+name+"</command-message>\n<command-args>"+args+"</command-args>"))
}

func commandStdoutTurn() *Obj {
	return litObj(`{"type": "user", "message": {"content": "<local-command-stdout>Set model to acme-model-large</local-command-stdout>"}}`)
}

func skillCall(skill string, sidechain bool) *Obj {
	e := ObjOf("type", "assistant")
	if sidechain {
		e.Set("isSidechain", true)
	}
	e.Set("message", ObjOf("content", []any{ObjOf("type", "tool_use", "name", "Skill", "input", ObjOf("skill", skill))}))
	return e
}

func assistantSaid(text string) *Obj {
	return ObjOf("type", "assistant", "message", ObjOf("content", []any{ObjOf("type", "text", "text", text)}))
}

func otherToolCall() *Obj {
	return litObj(`{"type": "assistant", "message": {"content": [{"type": "tool_use", "name": "Read", "input": {"file_path": "/x"}}]}}`)
}

func list(es ...*Obj) []any {
	out := make([]any, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out
}

func mountedSet(names ...string) Corpus {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return Corpus{Mounted: m}
}

func TestIsUserMessageCountsAHumanTurnAndNothingElse(t *testing.T) {
	if !IsUserMessage(humanSaid("do the thing")) {
		t.Fatal("a human turn")
	}
}

func TestIsUserMessageExcludesEveryNonHumanUserRoleShape(t *testing.T) {
	for _, c := range []struct {
		what  string
		entry *Obj
	}{
		{"a tool result", toolResultTurn()},
		{"an injected/meta turn", metaTurn("<system-reminder>…</system-reminder>")},
		{"a subagent sidechain turn", sidechainTurn("go")},
		{"a scheduled-task firing", scheduledTurn("Execute the Claudinite executor: " + legacyExecutorDoc)},
		{"a task notification", notifiedTurn("<task-notification>…")},
		{"a compaction summary", compactTurn()},
		{"a slash-command expansion", slash("model", "acme-model-large")},
		{"a local command's stdout", commandStdoutTurn()},
		{"an assistant turn", assistantSaid("sure")},
	} {
		if IsUserMessage(c.entry) {
			t.Errorf("%s must not count as a user message", c.what)
		}
	}
}

func TestCommandNameReadsTheTypedCommandOutOfItsExpansionAndOnlyFromThere(t *testing.T) {
	if n, ok := CommandName(slash("acme-skill-h", "")); !ok || n != "acme-skill-h" {
		t.Fatal(n)
	}
	if n, ok := CommandName(slash("model", "acme-model-large")); !ok || n != "model" {
		t.Fatal(n)
	}
	if n, ok := CommandName(humanSaid("run /acme-skill-h when you are done")); ok {
		t.Fatalf("prose that mentions a command is not one: %q", n)
	}
	if n, ok := CommandName(assistantSaid("use /review")); ok {
		t.Fatal(n)
	}
}

func TestSkillToolLoadsReadsTheSkillToolUseIgnoringEveryOtherTool(t *testing.T) {
	if got := SkillToolLoads(skillCall("acme-skill-i", false)); !reflect.DeepEqual(got, []string{"acme-skill-i"}) {
		t.Fatal(got)
	}
	if got := SkillToolLoads(otherToolCall()); len(got) != 0 {
		t.Fatal(got)
	}
	if got := SkillToolLoads(humanSaid("hi")); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestCountEntriesATypedCommandNamingAMountedSkillIsALoadABuiltInIsNot(t *testing.T) {
	counts := CountEntries(list(slash("acme-skill-h", ""), slash("model", "acme-model-large"), slash("clear", "")), mountedSet("acme-skill-h", "acme-skill-i"))
	if counts.UserCommands != 3 {
		t.Fatalf("every typed command counts as a command: %v", counts.UserCommands)
	}
	same(t, counts.SkillLoads, `{"acme-skill-h": 1}`)
}

func TestCountEntriesSubagentSkillLoadsCount(t *testing.T) {
	counts := CountEntries(list(skillCall("acme-skill-i", true), skillCall("acme-skill-i", false)), Corpus{})
	same(t, counts.SkillLoads, `{"acme-skill-i": 2}`)
}

func TestCountEntriesAWholeSessionEveryCounterAtOnce(t *testing.T) {
	counts := CountEntries(list(
		humanSaid("start"), otherToolCall(), toolResultTurn(),
		skillCall("acme-skill-f", false), assistantSaid("found it"),
		humanSaid("now merge"), slash("acme-skill-h", ""), skillCall("acme-skill-h", false),
		scheduledTurn("automated"), metaTurn("<system-reminder>"),
	), mountedSet("acme-skill-h", "acme-skill-f"))
	if counts.UserMessages != 2 || counts.UserCommands != 1 {
		t.Fatalf("%v %v", counts.UserMessages, counts.UserCommands)
	}
	same(t, counts.SkillLoads, `{"acme-skill-f": 1, "acme-skill-h": 2}`)
}

// --- check activations ------------------------------------------------------

const hookPass = "2026-07-28T22:14:43Z run=4421 Stop: start checks\n" +
	"2026-07-28T22:14:43Z run=4421 Stop: done exit=0 checks-passed\n"

const hookFail = "Stop hook feedback:\n[node $CLAUDE_PROJECT_DIR/engine/hooks/stop-command.mjs]: " +
	"2026-07-28T22:14:19Z run=3614 Stop: start checks\n" +
	"Claudinite conformance checks failed — fix these findings now, in this session:\n\n" +
	"[BLOCKING] comment-classification  (conversation)\n" +
	"  the reply to the owner's latest comment (\"lgtm…\") declares no `Comment class:` line\n" +
	"  Fix: state the classification explicitly\n" +
	"  More: packs/acme-pack/RULES.md\n\n" +
	"[BLOCKING] task-lifecycle  (branch)\n" +
	"  none of the 1 commit(s) since origin/main references an issue (#N)\n" +
	"  Fix: reference the issue in the commit message\n\n" +
	"2 blocking, 0 advisory (work scope: all vs origin/main).\n" +
	"2026-07-28T22:14:19Z run=3614 Stop: done exit=2 blocking-findings\n"

const ciLog = "2026-07-29T08:12:04.1234567Z ##[group]Run node engine/checks/check_the_world.mjs\n" +
	"2026-07-29T08:12:05.7654321Z [BLOCKING] task-lifecycle  (branch)\n" +
	"2026-07-29T08:12:05.7654322Z   none of the 1 commit(s) since origin/main references an issue (#N)\n" +
	"2026-07-29T08:12:05.7654323Z 1 blocking, 4 advisory (world scope: all vs origin/main).\n" +
	"2026-07-29T08:12:05.9000000Z ##[error]Process completed with exit code 1.\n"

func hookFeedback(text string) *Obj { return metaTurn(text) }

func hookSummary(text string) *Obj {
	return ObjOf("type", "system", "subtype", "stop_hook_summary", "hookErrors", []any{text})
}

func hookSuccess(stderr, stdout string) *Obj {
	return ObjOf("type", "attachment", "attachment", ObjOf("type", "hook_success", "hookName", "Stop", "hookEvent", "Stop",
		"stderr", stderr, "stdout", stdout, "exitCode", 0.0))
}

func bashCall(id, command string) *Obj {
	return ObjOf("type", "assistant", "message", ObjOf("content", []any{ObjOf("type", "tool_use", "id", id, "name", "Bash", "input", ObjOf("command", command))}))
}

func bashResult(id, stdout string) *Obj {
	return ObjOf("type", "user",
		"message", ObjOf("content", []any{ObjOf("type", "tool_result", "tool_use_id", id, "content", stdout)}),
		"toolUseResult", ObjOf("stdout", stdout, "stderr", "", "interrupted", false, "isImage", false))
}

func readResult(id, content string) *Obj {
	return ObjOf("type", "user",
		"message", ObjOf("content", []any{ObjOf("type", "tool_result", "tool_use_id", id, "content", content)}),
		"toolUseResult", ObjOf("type", "text", "file", ObjOf("content", content)))
}

func ciFetch(id string) *Obj {
	return ObjOf("type", "assistant", "message", ObjOf("content", []any{ObjOf("type", "tool_use", "id", id, "name", "mcp__github__get_job_logs", "input", ObjOf("job_id", 1.0))}))
}

func ciResult(id, logs string) *Obj {
	text := Stringify(ObjOf("job_id", 90518898761.0, "logs_content", logs))
	return ObjOf("type", "user",
		"message", ObjOf("content", []any{ObjOf("type", "tool_result", "tool_use_id", id, "content", []any{ObjOf("type", "text", "text", text)})}),
		"toolUseResult", []any{ObjOf("type", "text", "text", text)})
}

func scopeRow(over string) *Obj {
	return spread(litObj(`{"runs": 0, "failures": 0, "errors": 0, "blocking": 0, "advisory": 0, "ciRuns": 0, "ciFailures": 0}`), litObj(over))
}

func TestHookCheckRunsReadsTheHooksOwnCompletionLineWithTheOutcomeItDeclares(t *testing.T) {
	if got := HookCheckRuns(hookPass); !reflect.DeepEqual(got, []HookRun{{"2026-07-28T22:14:43Z 4421", 0, "checks-passed"}}) {
		t.Fatalf("%+v", got)
	}
	if got := HookCheckRuns(hookFail); !reflect.DeepEqual(got, []HookRun{{"2026-07-28T22:14:19Z 3614", 2, "blocking-findings"}}) {
		t.Fatalf("%+v", got)
	}
	if got := HookCheckRuns("2026-07-28T10:00:00Z run=7 Stop: done exit=0 loop-guard-relent"); got[0].Reason != "loop-guard-relent" {
		t.Fatal(got)
	}
	if got := HookCheckRuns("2026-07-28T10:00:00Z run=7 Stop: done exit=2 runner-error"); got[0].Reason != "runner-error" {
		t.Fatal(got)
	}
	if got := HookCheckRuns("2026-07-28T10:00:00Z run=7 Stop: start checks"); len(got) != 0 {
		t.Fatalf("a start is not a run: %v", got)
	}
}

func TestCheckSummariesReadsTheScopeAndTheFindingCountsOffTheSummaryLine(t *testing.T) {
	if got := CheckSummaries("2 blocking, 0 advisory (work scope: all vs origin/main)."); !reflect.DeepEqual(got, []Summary{{"work", 2, 0}}) {
		t.Fatal(got)
	}
	if got := CheckSummaries("# tests 810\n# pass 810\n0 blocking, 7 advisory (world scope: all vs origin/main)."); !reflect.DeepEqual(got, []Summary{{"world", 0, 7}}) {
		t.Fatal(got)
	}
	if got := CheckSummaries("the runner prints 2 blocking, 0 advisory (work scope: all) at the end"); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestFindingHeadersReadsTheRuleIDOffEachRenderedFinding(t *testing.T) {
	if got := FindingHeaders(hookFail); !reflect.DeepEqual(got, []Finding{{"blocking", "comment-classification"}, {"blocking", "task-lifecycle"}}) {
		t.Fatal(got)
	}
	if got := FindingHeaders("[ADVISORY] acme-skill-g  packs/x/y.mjs:3"); !reflect.DeepEqual(got, []Finding{{"advisory", "acme-skill-g"}}) {
		t.Fatal(got)
	}
}

func TestCheckInvocationsCountsRunnerInvocationsAndOnlyActualInvocations(t *testing.T) {
	for command, want := range map[string]map[string]float64{
		"node engine/checks/check_the_world.mjs 2>&1 | tail -40":                                  {"work": 0, "world": 1},
		"node .claudinite/shared/engine/checks/check_the_work.mjs >/tmp/out":                      {"work": 1, "world": 0},
		"node engine/checks/check_the_world.mjs | tail -3; node engine/checks/check_the_work.mjs": {"work": 1, "world": 1},
		`git ls-files | grep -i "check_the_world" | head`:                                         {"work": 0, "world": 0},
	} {
		if got := CheckInvocations(command); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", command, got)
		}
	}
}

func TestCheckOutputsDedupesOneHookExecutionRecordedUnderTwoEntryShapes(t *testing.T) {
	outputs := CheckOutputs(list(hookFeedback(hookFail), hookSummary(hookFail)))
	if len(outputs) != 1 || outputs[0].Source != "hook" {
		t.Fatalf("%+v", outputs)
	}
}

func TestCheckOutputsTakesBashResultsAndLeavesEveryOtherToolResultAlone(t *testing.T) {
	outputs := CheckOutputs(list(
		bashCall("t1", "node engine/checks/check_the_world.mjs"),
		bashResult("t1", "0 blocking, 7 advisory (world scope: all vs origin/main)."),
		readResult("t2", "[BLOCKING] acme-skill-g  x.mjs\n0 blocking, 1 advisory (world scope: all)."),
	))
	if len(outputs) != 1 || outputs[0].Command == nil || *outputs[0].Command != "node engine/checks/check_the_world.mjs" {
		t.Fatalf("%+v", outputs)
	}
}

func TestCountChecksAPassingHookRunIsAnActivationNotAFailure(t *testing.T) {
	c := CountChecks(list(hookSuccess(hookPass, "")))
	if !deepEqual(c.Checks.ObjAt("work"), scopeRow(`{"runs": 1}`)) {
		t.Fatal(Stringify(c.Checks))
	}
	if c.CheckFindings.Len() != 0 {
		t.Fatal("nothing was caught, so no rule has a key")
	}
}

func TestCountChecksAFailingHookRunIsOneActivationOneFailureAndItsRules(t *testing.T) {
	c := CountChecks(list(hookFeedback(hookFail), hookSummary(hookFail)))
	if !deepEqual(c.Checks.ObjAt("work"), scopeRow(`{"runs": 1, "failures": 1, "blocking": 2}`)) {
		t.Fatal(Stringify(c.Checks))
	}
	want := ObjOf("comment-classification", findingRow(`{"blocking": 1, "sessions": 1}`), "task-lifecycle", findingRow(`{"blocking": 1, "sessions": 1}`))
	if !deepEqual(c.CheckFindings, want) {
		t.Fatal(Stringify(c.CheckFindings))
	}
}

func TestCountChecksTheLoopGuardRelentIsAFailure(t *testing.T) {
	c := CountChecks(list(hookSuccess(
		"2026-07-28T10:00:00Z run=7 Stop: done exit=0 loop-guard-relent\n",
		"claudinite checks: the same blocking findings survived 2 fix attempts — letting the stop through.",
	)))
	if !deepEqual(c.Checks.ObjAt("work"), scopeRow(`{"runs": 1, "failures": 1}`)) {
		t.Fatal(Stringify(c.Checks))
	}
}

func TestCountChecksARunnerThatCouldNotLaunchIsAnError(t *testing.T) {
	c := CountChecks(list(hookFeedback("Stop hook feedback:\n[node …/stop-command.mjs]: 2026-07-28T10:00:00Z run=7 Stop: done exit=2 runner-error\n")))
	if !deepEqual(c.Checks.ObjAt("work"), scopeRow(`{"runs": 1, "errors": 1}`)) {
		t.Fatal(Stringify(c.Checks))
	}
}

func TestCountChecksWorldScopeRunsComeOffTheBashInvocation(t *testing.T) {
	c := CountChecks(list(
		bashCall("t1", "node engine/checks/check_the_world.mjs"),
		bashResult("t1", ""),
		bashCall("t2", "node engine/checks/check_the_world.mjs 2>&1 | tail -5"),
		bashResult("t2", "[BLOCKING] task-lifecycle  (branch)\n  …\n1 blocking, 4 advisory (world scope: all vs origin/main)."),
	))
	if !deepEqual(c.Checks.ObjAt("world"), scopeRow(`{"runs": 2, "failures": 1, "blocking": 1, "advisory": 4}`)) {
		t.Fatal(Stringify(c.Checks))
	}
}

func TestCountChecksARunnerWrappedInATestCommandIsCountedFromItsOutputNotDoubleCounted(t *testing.T) {
	wrapped := CountChecks(list(bashCall("t1", "npm test"), bashResult("t1", "0 blocking, 7 advisory (world scope: all vs origin/main).")))
	if v := field(wrapped.Checks, "world.runs"); v != 1.0 {
		t.Fatal(v)
	}
	named := CountChecks(list(
		bashCall("t1", "node engine/checks/check_the_world.mjs"),
		bashResult("t1", "0 blocking, 7 advisory (world scope: all vs origin/main)."),
	))
	if v := field(named.Checks, "world.runs"); v != 1.0 {
		t.Fatalf("named AND reported is still one run: %v", v)
	}
}

func TestAGitHubActionsTimestampPrefixDoesNotHideTheMarks(t *testing.T) {
	if got := CheckSummaries(ciLog); !reflect.DeepEqual(got, []Summary{{"world", 1, 4}}) {
		t.Fatal(got)
	}
	if got := FindingHeaders(ciLog); !reflect.DeepEqual(got, []Finding{{"blocking", "task-lifecycle"}}) {
		t.Fatal(got)
	}
}

func TestCheckOutputsDecodesTheJSONWrappedCIPayload(t *testing.T) {
	outputs := CheckOutputs(list(ciFetch("c1"), ciResult("c1", ciLog)))
	if len(outputs) == 0 || outputs[0].Source != "ci" {
		t.Fatalf("%+v", outputs)
	}
	if !strings.Contains(outputs[0].Text, "\n") {
		t.Fatal("the escaped newlines are real newlines by the time a mark reads them")
	}
	if got := CheckSummaries(outputs[0].Text); !reflect.DeepEqual(got, []Summary{{"world", 1, 4}}) {
		t.Fatal(got)
	}
}

func TestCountChecksACIRunTheSessionPulledTheLogForIsCounted(t *testing.T) {
	c := CountChecks(list(ciFetch("c1"), ciResult("c1", ciLog)))
	if !deepEqual(c.Checks.ObjAt("world"), scopeRow(`{"runs": 1, "failures": 1, "blocking": 1, "advisory": 4, "ciRuns": 1, "ciFailures": 1}`)) {
		t.Fatal(Stringify(c.Checks))
	}
	if !deepEqual(c.CheckFindings, ObjOf("task-lifecycle", findingRow(`{"blocking": 1, "sessions": 1}`))) {
		t.Fatal(Stringify(c.CheckFindings))
	}
}

func TestCountChecksTheCIShareStaysSeparable(t *testing.T) {
	c := CountChecks(list(
		bashCall("t1", "node engine/checks/check_the_world.mjs"), bashResult("t1", ""),
		ciFetch("c1"), ciResult("c1", ciLog),
	))
	runs, ci := field(c.Checks, "world.runs"), field(c.Checks, "world.ciRuns")
	if runs != 2.0 || ci != 1.0 || runs.(float64)-ci.(float64) != 1 {
		t.Fatalf("%v %v", runs, ci)
	}
}

func TestCountChecksReReadingOneCIJobLogIsStillOneRun(t *testing.T) {
	c := CountChecks(list(ciFetch("c1"), ciResult("c1", ciLog), ciFetch("c2"), ciResult("c2", ciLog)))
	if v := field(c.Checks, "world.runs"); v != 1.0 {
		t.Fatal(v)
	}
	next := CountChecks(list(ciFetch("c1"), ciResult("c1", ciLog), ciFetch("c2"), ciResult("c2", strings.ReplaceAll(ciLog, "08:12:05", "09:30:11"))))
	if v := field(next.Checks, "world.runs"); v != 2.0 {
		t.Fatalf("a genuinely later run differs by its Actions timestamps: %v", v)
	}
}

func TestCountChecksACILogCarryingNoCheckOutputAtAllIsNotARun(t *testing.T) {
	c := CountChecks(list(ciFetch("c1"), ciResult("c1",
		"2026-07-29T08:12:04.1234567Z ##[group]Run npm ci\n2026-07-29T08:12:09.0000000Z added 0 packages\n")))
	if c.Checks.Len() != 0 {
		t.Fatal(Stringify(c.Checks))
	}
}

func TestCountChecksAScopeThatNeverRanHasNoKey(t *testing.T) {
	c := CountChecks(list(hookSuccess(hookPass, "")))
	if !reflect.DeepEqual(c.Checks.Keys(), []string{"work"}) {
		t.Fatal(c.Checks.Keys())
	}
}

func TestCountEntriesCarriesTheCheckCountsAlongsideTheSkillCounts(t *testing.T) {
	counts := CountEntries(list(humanSaid("go"), skillCall("acme-skill-i", false), hookFeedback(hookFail)), mountedSet("acme-skill-i"))
	same(t, counts.SkillLoads, `{"acme-skill-i": 1}`)
	if field(counts.Checks, "work.failures") != 1.0 || field(counts.CheckFindings, "task-lifecycle.blocking") != 1.0 {
		t.Fatal(Stringify(counts.Checks), Stringify(counts.CheckFindings))
	}
}

// --- day folding --------------------------------------------------------------

// countsOf is a capture's counts spelled as the JSON the Node fixtures
// write; a group left out is one the file has no key for.
func countsOf(text string) Counts {
	o := litObj(text)
	num := func(k string) float64 {
		v, _ := o.Get(k)
		n, _ := v.(float64)
		return n
	}
	c := Counts{UserMessages: num("userMessages"), UserCommands: num("userCommands"),
		SkillLoads: o.ObjAt("skillLoads"), TaskExec: o.ObjAt("taskExec"), Checks: o.ObjAt("checks"),
		CheckFindings: o.ObjAt("checkFindings"), TokensByModel: o.ObjAt("tokensByModel")}
	if tk := o.ObjAt("tokens"); tk != nil {
		in, _ := tk.Get("input")
		out, _ := tk.Get("output")
		c.Tokens = &Tokens{in.(float64), out.(float64)}
	}
	if s := o.ObjAt("seconds"); s != nil {
		h, _ := s.Get("human")
		a, _ := s.Get("agent")
		c.Seconds = &Seconds{h.(float64), a.(float64)}
	}
	return c
}

func capture(date string, issue *float64, session, counts string) CaptureFile {
	return CaptureFile{Date: date, Issue: issue, SessionID: session, Counts: countsOf(counts)}
}

func foldDaysOf(t *testing.T, files ...CaptureFile) *Obj {
	t.Helper()
	days, err := FoldDays(files)
	if err != nil {
		t.Fatal(err)
	}
	return days
}

func TestFoldDaysAPRKeyedCaptureIsAMergeAndItsSessionIsUnresolved(t *testing.T) {
	keyed := capture("2026-07-28", nil, "s9", `{"userMessages": 3}`)
	keyed.PR = fp(1583)
	days := foldDaysOf(t, keyed, capture("2026-07-28", fp(0), "s9", `{}`))
	if field(days, "2026-07-28.merges") != 1.0 || field(days, "2026-07-28.sessions") != 1.0 {
		t.Fatal(Stringify(days))
	}
	same(t, field(days, "2026-07-28.taskCost"), `{"(unresolved)": {"sessions": 1, "userMessages": 3}}`)
}

func TestFoldDaysCapturesMergesAndDistinctSessionsPerDay(t *testing.T) {
	days := foldDaysOf(t,
		capture("2026-07-28", fp(12), "s1", `{"userMessages": 4, "skillLoads": {"a": 1}}`),
		capture("2026-07-28", fp(0), "s1", `{"userMessages": 2, "skillLoads": {"a": 1, "b": 3}}`),
		capture("2026-07-28", fp(13), "s2", `{"userMessages": 5, "userCommands": 1}`),
		capture("2026-07-27", fp(0), "s0", `{}`),
	)
	want := spread(litObj(`{"captures": 3, "merges": 2, "sessions": 2, "userMessages": 11, "userCommands": 1}`), emptyGroups(),
		litObj(`{"skillLoads": {"a": 2, "b": 3}, "taskCost": {"(unresolved)": {"sessions": 2, "userMessages": 11}}}`))
	if !deepEqual(days.ObjAt("2026-07-28"), want) {
		t.Fatalf("got  %s\nwant %s", Stringify(days.ObjAt("2026-07-28")), Stringify(want))
	}
	if field(days, "2026-07-27.merges") != 0.0 {
		t.Fatal(Stringify(days))
	}
}

func TestFoldDaysSumsTheCheckActivationsAcrossADaysCaptureFiles(t *testing.T) {
	days := foldDaysOf(t,
		capture("2026-07-28", fp(12), "s1", `{
			"checks": {"work": {"runs": 4, "failures": 2, "errors": 0, "blocking": 3, "advisory": 0}, "world": {"runs": 1, "failures": 0, "errors": 0, "blocking": 0, "advisory": 0}},
			"checkFindings": {"task-lifecycle": {"blocking": 2, "advisory": 0}}}`),
		capture("2026-07-28", fp(13), "s2", `{
			"checks": {"work": {"runs": 2, "failures": 1, "errors": 0, "blocking": 1, "advisory": 0}},
			"checkFindings": {"task-lifecycle": {"blocking": 1, "advisory": 0}, "acme-skill-g": {"blocking": 0, "advisory": 5}}}`),
	)
	same(t, field(days, "2026-07-28.checks.work"), `{"runs": 6, "failures": 3, "errors": 0, "blocking": 4, "advisory": 0}`)
	same(t, field(days, "2026-07-28.checks.world"), `{"runs": 1, "failures": 0, "errors": 0, "blocking": 0, "advisory": 0}`)
	same(t, field(days, "2026-07-28.checkFindings"), `{
		"task-lifecycle": {"blocking": 3, "advisory": 0, "sessions": 2},
		"acme-skill-g": {"blocking": 0, "advisory": 5, "sessions": 1}}`)
}

func TestFoldDaysIsAPureRecompute(t *testing.T) {
	files := []CaptureFile{capture("2026-07-28", fp(1), "s1", `{"userMessages": 3, "skillLoads": {"a": 1}}`)}
	if a, b := foldDaysOf(t, files...), foldDaysOf(t, files...); !deepEqual(a, b) {
		t.Fatalf("%s\n%s", Stringify(a), Stringify(b))
	}
}

// --- week folding ---------------------------------------------------------------

func TestIsoWeekPutsADateInItsISO8601WeekIncludingTheYearBoundary(t *testing.T) {
	for date, want := range map[string]string{
		"2026-07-28": "2026-W31", "2026-07-26": "2026-W30", "2026-07-27": "2026-W31",
		"2027-01-01": "2026-W53", "2026-01-01": "2026-W01",
	} {
		if got := IsoWeek(date); got != want {
			t.Errorf("%s: %s, want %s", date, got, want)
		}
	}
}

func TestDaysToFoldTakesEveryDayThatClosedSinceTheWatermarkInOrder(t *testing.T) {
	days := litObj(`{"2026-07-25": {}, "2026-07-26": {}, "2026-07-27": {}, "2026-07-28": {}}`)
	if got := DaysToFold(days, "2026-07-25", "2026-07-28"); !reflect.DeepEqual(got, []string{"2026-07-26", "2026-07-27"}) {
		t.Fatal(got)
	}
	if got := DaysToFold(days, nil, "2026-07-28"); !reflect.DeepEqual(got, []string{"2026-07-25", "2026-07-26", "2026-07-27"}) {
		t.Fatal(got)
	}
	if got := DaysToFold(days, "2026-07-27", "2026-07-28"); len(got) != 0 {
		t.Fatalf("today is never folded: %v", got)
	}
}

func addDays(t *testing.T, week any, days ...*Obj) *Obj {
	t.Helper()
	for _, d := range days {
		w, err := AddDayToWeek(week, d)
		if err != nil {
			t.Fatal(err)
		}
		week = w
	}
	return week.(*Obj)
}

func TestAddDayToWeekSumsTheCountersAndDeclaresHowManyDaysItAbsorbed(t *testing.T) {
	day := func() *Obj {
		return litObj(`{"captures": 2, "merges": 1, "sessions": 2, "userMessages": 10, "userCommands": 1, "skillLoads": {"a": 1},
			"checks": {"work": {"runs": 3, "failures": 1, "errors": 0, "blocking": 1, "advisory": 0}},
			"checkFindings": {"task-lifecycle": {"blocking": 1, "advisory": 0}}}`)
	}
	week := addDays(t, nil, day(), day())
	want := spread(litObj(`{"days": 2, "captures": 4, "merges": 2, "sessionDays": 4, "userMessages": 20, "userCommands": 2}`), emptyGroups(),
		litObj(`{"skillLoads": {"a": 2}, "checks": {"work": {"runs": 6, "failures": 2, "errors": 0, "blocking": 2, "advisory": 0}},
			"checkFindings": {"task-lifecycle": {"blocking": 2, "advisory": 0}}}`))
	if !deepEqual(week, want) {
		t.Fatalf("got  %s\nwant %s", Stringify(week), Stringify(want))
	}
}

func TestAddDayToWeekExtendsAWeekFoldedBeforeTheChecksWereCounted(t *testing.T) {
	old := litObj(`{"days": 3, "captures": 6, "merges": 5, "sessionDays": 4, "userMessages": 50, "userCommands": 2, "skillLoads": {"a": 1}}`)
	week := addDays(t, old, litObj(`{"captures": 1, "merges": 1, "sessions": 1, "userMessages": 5, "userCommands": 0, "skillLoads": {},
		"checks": {"work": {"runs": 2, "failures": 1, "errors": 0, "blocking": 1, "advisory": 0}},
		"checkFindings": {"task-lifecycle": {"blocking": 1, "advisory": 0}}}`))
	if field(week, "days") != 4.0 {
		t.Fatal(Stringify(week))
	}
	same(t, field(week, "checks.work"), `{"runs": 2, "failures": 1, "errors": 0, "blocking": 1, "advisory": 0}`)
	if field(week, "userMessages") != 55.0 {
		t.Fatal("and the counters it already had keep summing")
	}
}

// --- the whole fold -------------------------------------------------------------

func foldOf(t *testing.T, in FoldIn) UsageFile {
	t.Helper()
	out, err := FoldUsage(in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFoldUsageDaysRecomputeWeeksAppendOnceWatermarkAdvances(t *testing.T) {
	files := []CaptureFile{
		capture("2026-07-26", fp(1), "s1", `{"userMessages": 3, "skillLoads": {"acme-skill-h": 1}}`),
		capture("2026-07-27", fp(2), "s2", `{"userMessages": 5}`),
		capture("2026-07-28", fp(0), "s3", `{"userMessages": 1}`),
	}
	first := foldOf(t, FoldIn{Files: files, Today: "2026-07-28"})
	if first.FoldedThrough != "2026-07-27" || !reflect.DeepEqual(first.Weeks.Keys(), []string{"2026-W30", "2026-W31"}) {
		t.Fatalf("%v %v", first.FoldedThrough, first.Weeks.Keys())
	}
	if field(first.Weeks, "2026-W30.days") != 1.0 {
		t.Fatal(Stringify(first.Weeks))
	}
	same(t, field(first.Weeks, "2026-W30.skillLoads"), `{"acme-skill-h": 1}`)
	if !first.Days.Has("2026-07-28") {
		t.Fatal("today still has its day row")
	}

	more := append(append([]CaptureFile{}, files...), capture("2026-07-28", fp(4), "s4", `{"userMessages": 2}`))
	second := foldOf(t, FoldIn{Files: more, Prior: first, Today: "2026-07-28"})
	if !deepEqual(second.Weeks, first.Weeks) {
		t.Fatal("a re-run folds no closed day twice")
	}
	if field(second.Days, "2026-07-28.captures") != 2.0 {
		t.Fatal("today recomputes to include the new capture")
	}

	third := foldOf(t, FoldIn{Files: more, Prior: second, Today: "2026-07-29"})
	if third.FoldedThrough != "2026-07-28" || field(third.Weeks, "2026-W31.days") != 2.0 || field(third.Weeks, "2026-W31.userMessages") != 8.0 {
		t.Fatalf("%v %s", third.FoldedThrough, Stringify(third.Weeks))
	}
}

func TestFoldUsageADayWhoseRawFilesAgedOutKeepsItsWeekRowAndDropsItsDayRow(t *testing.T) {
	prior := foldOf(t, FoldIn{Files: []CaptureFile{capture("2026-07-20", fp(1), "s1", `{"userMessages": 9}`)}, Today: "2026-07-21"})
	if field(prior.Weeks, "2026-W30.userMessages") != 9.0 {
		t.Fatal(Stringify(prior.Weeks))
	}
	later := foldOf(t, FoldIn{Prior: prior, Today: "2026-08-01"})
	if later.Days.Len() != 0 {
		t.Fatal("the day row is gone with its raw backing")
	}
	if field(later.Weeks, "2026-W30.userMessages") != 9.0 {
		t.Fatal("its week row carries it")
	}
	if later.FoldedThrough != "2026-07-20" {
		t.Fatal("and the watermark does not skip forward over days it never saw")
	}
}

func TestTheWrittenFileSortsEveryKeySoAnUnchangedRecomputeIsByteIdentical(t *testing.T) {
	files := []CaptureFile{
		capture("2026-07-27", fp(2), "s2", `{"skillLoads": {"zeta": 1, "alpha": 2}}`),
		capture("2026-07-26", fp(1), "s1", `{"skillLoads": {"middle": 1}}`),
	}
	written := func(in []CaptureFile) string {
		return RenderUsageFile(EncodeUsage(foldOf(t, FoldIn{Files: in, Today: "2026-07-28"})))
	}
	a := written(files)
	if b := written([]CaptureFile{files[1], files[0]}); a != b {
		t.Fatalf("%s\n%s", a, b)
	}
	if keys := lit(a).(*Obj).ObjAt("days").ObjAt("2026-07-27").ObjAt("skillLoads").Keys(); !reflect.DeepEqual(keys, []string{"alpha", "zeta"}) {
		t.Fatal(keys)
	}
}

func TestFoldUsageAMountedSkillThatNeverLoadsHasNoKey(t *testing.T) {
	folded := foldOf(t, FoldIn{Files: []CaptureFile{capture("2026-07-27", fp(1), "s1", `{"skillLoads": {"acme-skill-h": 1}}`)}, Today: "2026-07-28"})
	loads := folded.Days.ObjAt("2026-07-27").ObjAt("skillLoads")
	if !reflect.DeepEqual(loads.Keys(), []string{"acme-skill-h"}) {
		t.Fatal(loads.Keys())
	}
	var never []string
	for _, s := range []string{"acme-skill-h", "acme-skill-i", "acme-skill-f"} {
		if !loads.Has(s) {
			never = append(never, s)
		}
	}
	if !reflect.DeepEqual(never, []string{"acme-skill-i", "acme-skill-f"}) {
		t.Fatal(never)
	}
}

// --- what the session cost ----------------------------------------------------------

func spent(model string, usage string) *Obj {
	m := ObjOf("content", []any{}, "usage", litObj(usage))
	if model != "" {
		m = ObjOf("model", model, "content", []any{}, "usage", litObj(usage))
	}
	return ObjOf("type", "assistant", "message", m)
}

func TestTokensInSumsTheUsageRecordsTheTranscriptCarriesAndProbesRatherThanAssumes(t *testing.T) {
	got, ok := TokensIn(list(
		spent("", `{"input_tokens": 10, "output_tokens": 3, "cache_read_input_tokens": 100}`),
		spent("", `{"input_tokens": 5, "output_tokens": 2, "cache_creation_input_tokens": 40}`),
	))
	if !ok || got != (Tokens{155, 5}) {
		t.Fatal(got, ok)
	}
	if _, ok := TokensIn(list(litObj(`{"type": "assistant", "message": {"content": []}}`), humanSaid("hi"))); ok {
		t.Fatal("a transcript recording no usage reports nothing")
	}
	if _, ok := TokensIn(nil); ok {
		t.Fatal("an empty transcript reports nothing")
	}
}

func TestCountEntriesCarriesTheSpendAndFoldDaysCountsItOncePerSession(t *testing.T) {
	counts := CountEntries(list(litObj(`{"type": "assistant", "message": {"content": [], "usage": {"input_tokens": 7, "output_tokens": 1}}}`)), Corpus{})
	if counts.Tokens == nil || *counts.Tokens != (Tokens{7, 1}) {
		t.Fatal(counts.Tokens)
	}
	file := func(issue float64, extra string) CaptureFile {
		return CaptureFile{Date: "2026-08-20", Stamp: "2026-08-20T09:30:00Z", Issue: fp(issue), SessionID: "sess-1", Counts: countsOf(extra)}
	}
	days := foldDaysOf(t, file(12, `{"tokens": {"input": 100, "output": 10}}`), file(0, `{"tokens": {"input": 260, "output": 30}}`))
	day := days.ObjAt("2026-08-20")
	if field(day, "captures") != 2.0 || field(day, "sessions") != 1.0 || field(day, "tokensIn") != 260.0 || field(day, "tokenSessions") != 1.0 {
		t.Fatal(Stringify(day))
	}
}

func TestADayWhoseTranscriptsCarriedNoUsageRecordsHasNoTokenKeysAtAll(t *testing.T) {
	days := foldDaysOf(t, CaptureFile{Date: "2026-08-20", Stamp: "2026-08-20T09:30:00Z", Issue: fp(0), SessionID: "s1",
		Counts: countsOf(`{"userMessages": 1, "tokens": null}`)})
	day := days.ObjAt("2026-08-20")
	if day.Has("tokensIn") || day.Has("tokenSessions") {
		t.Fatal("unknown is a state, not a zero")
	}
}

func TestTokensByModelInSplitsTheSameSpendByTheModelThatWasBilledForIt(t *testing.T) {
	es := list(
		spent("acme-model-large", `{"input_tokens": 10, "output_tokens": 3, "cache_read_input_tokens": 100}`),
		spent("acme-model-large", `{"input_tokens": 5, "output_tokens": 2, "cache_creation_input_tokens": 40}`),
		spent("acme-model-small", `{"input_tokens": 1, "output_tokens": 1}`),
	)
	same(t, TokensByModelIn(es), `{
		"acme-model-large": {"input": 15, "cacheRead": 100, "cacheCreate": 40, "output": 5},
		"acme-model-small": {"input": 1, "cacheRead": 0, "cacheCreate": 0, "output": 1}}`)
	if total, _ := TokensIn(es); total != (Tokens{15 + 100 + 40 + 1, 6}) {
		t.Fatal(total)
	}
}

func TestTokensByModelInKeysAnUnattributedSpendAsItsOwnStateAndAnswersNullForNone(t *testing.T) {
	same(t, TokensByModelIn(list(spent("", `{"input_tokens": 4, "output_tokens": 1}`))),
		`{"`+TokensByModelUnknown+`": {"input": 4, "cacheRead": 0, "cacheCreate": 0, "output": 1}}`)
	if got := TokensByModelIn(list(litObj(`{"type": "assistant", "message": {"content": []}}`))); got != nil {
		t.Fatal(Stringify(got))
	}
	if got := TokensByModelIn(nil); got != nil {
		t.Fatal(Stringify(got))
	}
}

func stampedAt(entry *Obj, at time.Time, extra ...any) *Obj {
	e := spread(entry, ObjOf(extra...))
	e.Set("timestamp", at.UTC().Format("2006-01-02T15:04:05.000Z"))
	return e
}

func secondAt(s int) time.Time { return time.Date(2026, 8, 20, 9, 0, s, 0, time.UTC) }

func TestTurnSecondsBillsEachTurnTheGapThatProducedItAndCapsTheHumanSide(t *testing.T) {
	got, ok := TurnSeconds(list(
		stampedAt(humanSaid("do the thing"), secondAt(0)),
		stampedAt(assistantSaid("on it"), secondAt(30)),
		stampedAt(humanSaid("and this too"), secondAt(90)),
		stampedAt(assistantSaid("done"), secondAt(100)),
	), HumanSecondsCap)
	if !ok || got != (Seconds{60, 40}) {
		t.Fatal(got, ok)
	}
}

func TestTurnSecondsCapsAnOvernightGapSoTheHumanFigureStaysAFloor(t *testing.T) {
	minute := func(m int) time.Time { return time.Date(2026, 8, 20, 9, m, 0, 0, time.UTC) }
	got, _ := TurnSeconds(list(stampedAt(assistantSaid("done"), minute(0)), stampedAt(humanSaid("back"), minute(600))), HumanSecondsCap)
	if got.Human != 600 {
		t.Fatalf("the cap, in seconds: %v", got.Human)
	}
}

func TestTurnSecondsExcludesASubagentSidechain(t *testing.T) {
	got, ok := TurnSeconds(list(
		stampedAt(humanSaid("go"), secondAt(0)),
		stampedAt(assistantSaid("spawning"), secondAt(10)),
		stampedAt(assistantSaid("subagent working"), secondAt(20), "isSidechain", true),
		stampedAt(assistantSaid("done"), secondAt(40)),
	), HumanSecondsCap)
	if !ok || got != (Seconds{0, 40}) {
		t.Fatal(got, ok)
	}
}

func TestTurnSecondsAnswersNullNeverZeroForATranscriptThatCarriesNoTimestamps(t *testing.T) {
	if _, ok := TurnSeconds(list(humanSaid("hi"), assistantSaid("hello")), HumanSecondsCap); ok {
		t.Fatal("no timestamps")
	}
	if _, ok := TurnSeconds(nil, HumanSecondsCap); ok {
		t.Fatal("no entries")
	}
}

func TestTaskCostKeyNamesTheTaskAndKeepsTheTwoUnknownLanesApart(t *testing.T) {
	if got := TaskCostKey(litObj(`{"tidy-repo/tidy-issues": {}}`), 42.0); got != "tidy-repo/tidy-issues" {
		t.Fatal(got)
	}
	if got := TaskCostKey(NewObj(), 0.0); got != TaskCostNone {
		t.Fatal(got)
	}
	if got := TaskCostKey(NewObj(), 42.0); got != TaskCostUnresolved {
		t.Fatal(got)
	}
}

func costFile(issue float64, session, extra string) CaptureFile {
	return CaptureFile{Date: "2026-08-20", Stamp: "2026-08-20T09:30:00Z", Issue: fp(issue), SessionID: session, Counts: countsOf(extra)}
}

func TestFoldDaysSplitsCostPerTaskPerSessionAndNeverDemotesANamedTask(t *testing.T) {
	days := foldDaysOf(t,
		costFile(12, "sess-1", `{"taskExec": {"tidy-repo/tidy-issues": {"success": 1}}, "userMessages": 2, "tokens": {"input": 100, "output": 10}}`),
		costFile(0, "sess-1", `{"userMessages": 1, "tokens": {"input": 260, "output": 30}}`),
		costFile(0, "sess-2", `{"userMessages": 7, "tokens": {"input": 50, "output": 5}}`),
	)
	same(t, field(days, "2026-08-20.taskCost"), `{
		"tidy-repo/tidy-issues": {"sessions": 1, "userMessages": 3, "tokensIn": 260, "tokensOut": 30},
		"`+TaskCostNone+`": {"sessions": 1, "userMessages": 7, "tokensIn": 50, "tokensOut": 5}}`)
	if field(days, "2026-08-20.tokensIn") != 310.0 {
		t.Fatal(Stringify(days))
	}
}

func TestATaskWhoseSessionsAttestedNoSpendCarriesNoTokenColumns(t *testing.T) {
	days := foldDaysOf(t, costFile(0, "s1", `{"userMessages": 4, "tokens": null}`))
	same(t, field(days, "2026-08-20.taskCost"), `{"`+TaskCostNone+`": {"sessions": 1, "userMessages": 4}}`)
}

func TestFoldDaysSumsTheWallClockAndThePerModelSplitFromTheWinningCapture(t *testing.T) {
	days := foldDaysOf(t,
		costFile(0, "s1", `{"tokens": {"input": 100, "output": 10}, "tokensByModel": {"m1": {"input": 100, "cacheRead": 0, "cacheCreate": 0, "output": 10}}, "seconds": {"human": 30, "agent": 60}}`),
		costFile(0, "s1", `{"tokens": {"input": 260, "output": 30}, "tokensByModel": {"m1": {"input": 260, "cacheRead": 0, "cacheCreate": 0, "output": 30}}, "seconds": {"human": 90, "agent": 200}}`),
	)
	day := days.ObjAt("2026-08-20")
	same(t, field(day, "tokensByModel"), `{"m1": {"input": 260, "cacheRead": 0, "cacheCreate": 0, "output": 30}}`)
	if field(day, "humanSeconds") != 90.0 || field(day, "agentSeconds") != 200.0 {
		t.Fatal(Stringify(day))
	}
}

func TestADayWhoseSessionsCarriedNoTimestampsHasNoWallClockKeys(t *testing.T) {
	days := foldDaysOf(t, costFile(0, "s1", `{"userMessages": 1, "seconds": null}`))
	if day := days.ObjAt("2026-08-20"); day.Has("humanSeconds") || day.Has("agentSeconds") {
		t.Fatal("an unknown span is not a zero one")
	}
	if EncodeUsage(UsageFile{Days: days, Weeks: NewObj(), Hours: NewObj()}).ObjAt("days").ObjAt("2026-08-20").Has("tokensByModel") {
		t.Fatal("an empty group map is never written at all")
	}
}

func TestFoldPrsFilesEachMergedPRUnderItsMergeDayKeepingUnknownEndsAbsent(t *testing.T) {
	days := NewObj()
	FoldPrs(days, NewObj(), []PrRecord{
		{Date: "2026-08-20", Number: 1583.0, LeadHours: fp(14.2), IssueLeadHours: fp(38.5), SessionToMerge: fp(0.7)},
		{Date: "2026-08-20", Number: 1584.0, LeadHours: fp(2.5)},
	}, "2026-08-21")
	same(t, field(days, "2026-08-20.prs"), `{
		"1583": {"leadHours": 14.2, "issueLeadHours": 38.5, "sessionToMergeHours": 0.7},
		"1584": {"leadHours": 2.5}}`)
}

func TestFoldPrsCarriesPriorRowsForwardAndAgesThemOutWithTheDayWindow(t *testing.T) {
	days := NewObj()
	FoldPrs(days, litObj(`{"2026-08-19": {"prs": {"1580": {"leadHours": 3}}}, "2026-06-01": {"prs": {"1400": {"leadHours": 9}}}}`), nil, "2026-08-21")
	same(t, field(days, "2026-08-19.prs"), `{"1580": {"leadHours": 3}}`)
	if days.Has("2026-06-01") {
		t.Fatal("past the day window, the week row is what keeps it")
	}
}

func TestFoldQueueOutcomesCountsEachParkKindOncePerItemAndUnknownParksStayUnknown(t *testing.T) {
	days := NewObj()
	FoldQueueOutcomes(days, NewObj(), []QueueRecord{
		{Date: "2026-08-20", Pack: "p", Task: "t", Outcome: "done", Parks: []string{"approval", "approval", "failure"}},
		{Date: "2026-08-20", Pack: "p", Task: "t", Outcome: "done", Parks: []string{"approval"}},
		{Date: "2026-08-20", Pack: "p", Task: "u", Outcome: "done"},
	}, "2026-08-21")
	day := days.ObjAt("2026-08-20")
	same(t, day.ObjAt("parks").ObjAt("p/t"), `{"failure": 1, "action": 0, "decision": 0, "approval": 2}`)
	if day.ObjAt("parks").Has("p/u") {
		t.Fatal("an unreadable listing leaves no key")
	}
	if field(day.ObjAt("queue").ObjAt("p/u"), "done") != 1.0 {
		t.Fatal("and costs the item nothing else")
	}
}

// --- the sources outside the capture files ---------------------------------------

func TestFoldDayFieldsWritesOnlyTheDaysASourceCouldSpeakFor(t *testing.T) {
	days := ObjOf("2026-08-20", blankDay())
	FoldDayFields(days, litObj(`{
		"2026-08-20": {"commits": 3, "linesAdded": 40, "linesRemoved": 5},
		"2026-08-19": {"commits": 0, "linesAdded": 0, "linesRemoved": 0}}`))
	if field(days, "2026-08-20.commits") != 3.0 {
		t.Fatal(Stringify(days))
	}
	if field(days, "2026-08-19.commits") != 0.0 {
		t.Fatal("a covered day with nothing in it is a real zero")
	}
	if days.ObjAt("2026-08-20").Has("releases") {
		t.Fatal("a source that said nothing leaves no key")
	}
}

func TestAddDayToWeekAddsNothingForAFieldTheDayHasNoOpinionOn(t *testing.T) {
	known := spread(blankDay(), litObj(`{"captures": 1, "commits": 4, "linesAdded": 10, "linesRemoved": 2}`))
	unknown := spread(blankDay(), litObj(`{"captures": 1}`))
	week := addDays(t, nil, known, unknown)
	if field(week, "days") != 2.0 || field(week, "captures") != 2.0 || field(week, "commits") != 4.0 || field(week, "linesAdded") != 10.0 {
		t.Fatal(Stringify(week))
	}
	if week.Has("releases") || week.Has("tokensIn") {
		t.Fatal(strings.Join(week.Keys(), ","))
	}
}

// --- the queue's own outcome record ----------------------------------------------

func closedRec(date, task, outcome, pack string) QueueRecord {
	return QueueRecord{Date: date, Pack: pack, Task: task, Outcome: outcome}
}

func TestFoldQueueOutcomesCountsEachClosedItemUnderItsTaskAndOutcomeWord(t *testing.T) {
	days := NewObj()
	FoldQueueOutcomes(days, NewObj(), []QueueRecord{
		closedRec("2026-08-20", "usage-fold", "done", "acme-pack-b"),
		closedRec("2026-08-20", "usage-fold", "done", "acme-pack-b"),
		closedRec("2026-08-20", "acme-task", "delivered", "acme-pack-b"),
		closedRec("2026-08-19", "acme-task", "none", "acme-pack-b"),
	}, "2026-08-20")
	same(t, days.ObjAt("2026-08-20").ObjAt("queue").ObjAt("acme-pack-b/usage-fold"), `{"done": 2, "delivered": 0, "obsolete": 0, "none": 0}`)
	if field(days.ObjAt("2026-08-20").ObjAt("queue").ObjAt("acme-pack-b/acme-task"), "delivered") != 1.0 {
		t.Fatal(Stringify(days))
	}
	if field(days.ObjAt("2026-08-19").ObjAt("queue").ObjAt("acme-pack-b/acme-task"), "none") != 1.0 {
		t.Fatal(Stringify(days))
	}
	if field(days, "2026-08-19.captures") != 0.0 {
		t.Fatal("a day with queue activity and no captures still gets a row")
	}
}

func TestFoldQueueOutcomesAppendsOntoWhatEarlierFoldsCountedAndAgesRowsOut(t *testing.T) {
	prior := litObj(`{
		"2026-08-20": {"queue": {"p/t": {"done": 2, "delivered": 0, "obsolete": 0, "none": 0}}},
		"2026-06-01": {"queue": {"p/ancient": {"done": 9, "delivered": 0, "obsolete": 0, "none": 0}}}}`)
	days := NewObj()
	FoldQueueOutcomes(days, prior, []QueueRecord{closedRec("2026-08-20", "t", "done", "p")}, "2026-08-20")
	if field(days.ObjAt("2026-08-20").ObjAt("queue").ObjAt("p/t"), "done") != 3.0 {
		t.Fatal("appended, never recomputed")
	}
	if days.Has("2026-06-01") {
		t.Fatal("past the day window, its week row carries it now")
	}
}

func TestTheQueueVocabularyMatchesWhatTheEnginesOwnDecoderCanReturn(t *testing.T) {
	word := func(labels ...string) string {
		if w := (workitem.Issue{Labels: labels}).Outcome(); w != "" {
			return w
		}
		return "none"
	}
	seen := map[string]bool{word(): true}
	for _, l := range []string{workitem.OutcomeDone, workitem.OutcomeDelivered, workitem.OutcomeObsolete} {
		seen[word(l)] = true
	}
	var words []string
	for w := range seen {
		words = append(words, w)
	}
	want := append([]string{}, QueueOutcomes...)
	sort.Strings(words)
	sort.Strings(want)
	if !reflect.DeepEqual(words, want) {
		t.Fatalf("%v, want %v", words, want)
	}
}

// --- the hour tier ----------------------------------------------------------------

func captureHoursOf(t *testing.T, text string) *Obj {
	out := NewObj()
	src := jsObj(t, text)
	for _, k := range src.Keys() {
		row := src.ObjAt(k)
		a, _ := row.Get("agentic")
		out.Set(k, &CaptureHour{Agentic: a.(float64), TaskExec: row.ObjAt("taskExec")})
	}
	return out
}

func TestFoldHoursCountsRunsByWorkflowAndSessionsByTheirCaptureStamp(t *testing.T) {
	hours := FoldHours(NewObj(), []Run{
		{StartedAt: "2026-08-21T10:03:00Z", Workflow: "scheduler", Conclusion: "success", ID: 1.0},
		{StartedAt: "2026-08-21T10:41:00Z", Workflow: "executor", Conclusion: "failure", ID: 2.0},
		{StartedAt: "2026-08-21T11:03:00Z", Workflow: "scheduler", Conclusion: "success", ID: 3.0},
	}, captureHoursOf(t, `{"2026-08-21T10": {"agentic": 2, "taskExec": {"p/t": {"success": 1, "failed": 0, "task-gone": 0, "invalid": 0}}}}`), "2026-08-21T11:30:00Z")
	same(t, hours.ObjAt("2026-08-21T10"), `{"scheduler": 1, "executor": 1, "agentic": 2, "failed": 1,
		"taskExec": {"p/t": {"success": 1, "failed": 0, "task-gone": 0, "invalid": 0}}}`)
	same(t, hours.ObjAt("2026-08-21T11"), `{"scheduler": 1, "executor": 0, "agentic": 0, "failed": 0, "taskExec": {}}`)
}

func TestFoldHoursAppendsRunCountsOntoPriorRowsButOverwritesTheRecomputedOnes(t *testing.T) {
	hours := FoldHours(jsObj(t, `{"2026-08-21T10": {"scheduler": 1, "executor": 0, "agentic": 2, "failed": 0, "taskExec": {"p/t": {"success": 1}}}}`),
		[]Run{{StartedAt: "2026-08-21T10:41:00Z", Workflow: "executor", Conclusion: "success", ID: 2.0}},
		captureHoursOf(t, `{"2026-08-21T10": {"agentic": 2, "taskExec": {"p/t": {"success": 1, "failed": 0, "task-gone": 0, "invalid": 0}}}}`),
		"2026-08-21T11:30:00Z")
	row := hours.ObjAt("2026-08-21T10")
	if field(row, "scheduler") != 1.0 || field(row, "executor") != 1.0 || field(row, "agentic") != 2.0 || field(row.ObjAt("taskExec").ObjAt("p/t"), "success") != 1.0 {
		t.Fatal(Stringify(row))
	}
}

func TestFoldHoursKeepsThreeDaysOfRowsAndDropsEverythingOlder(t *testing.T) {
	now := "2026-08-21T11:30:00Z"
	for key, in := range map[string]bool{"2026-08-21T11": true, "2026-08-18T12": true, "2026-08-18T11": false, "2026-08-21T12": false} {
		if WithinHourWindow(key, now, HourWindowHours) != in {
			t.Errorf("%s: want %v", key, in)
		}
	}
	hours := FoldHours(jsObj(t, `{"2026-08-10T04": {"scheduler": 9, "executor": 0, "agentic": 0, "failed": 0, "taskExec": {}}}`),
		[]Run{{StartedAt: "2026-08-10T05:00:00Z", Workflow: "scheduler", Conclusion: "success", ID: 1.0}}, NewObj(), now)
	if hours.Len() != 0 {
		t.Fatal(Stringify(hours))
	}
}

func TestCaptureHoursFilesEachSessionUnderTheHourItsCaptureNameStamps(t *testing.T) {
	file := func(stamp, session, exec string) CaptureFile {
		return CaptureFile{Date: stamp[:10], Stamp: stamp, Issue: fp(1), SessionID: session, Counts: countsOf(`{"taskExec": ` + exec + `}`)}
	}
	hours := CaptureHours([]CaptureFile{
		file("2026-08-21T10:03:00Z", "s1", `{"p/t": {"success": 1, "failed": 0, "task-gone": 0, "invalid": 0}}`),
		file("2026-08-21T10:44:00Z", "s1", `{}`),
		file("2026-08-21T10:55:00Z", "s2", `{}`),
		file("2026-08-21T11:02:00Z", "s3", `{}`),
	})
	ten, _ := hours.Get("2026-08-21T10")
	eleven, _ := hours.Get("2026-08-21T11")
	if ten.(*CaptureHour).Agentic != 2 || field(ten.(*CaptureHour).TaskExec, "p/t") == nil || field(ten.(*CaptureHour).TaskExec.ObjAt("p/t"), "success") != 1.0 || eleven.(*CaptureHour).Agentic != 1 {
		t.Fatalf("%+v %+v", ten, eleven)
	}
}

// --- the whole fold, every source ---------------------------------------------------

func strp(s string) *string { return &s }

func TestFoldUsageCarriesEveryWatermarkAndTheStampItWasHanded(t *testing.T) {
	first := foldOf(t, FoldIn{Today: "2026-08-21", Now: strp("2026-08-21T11:00:00Z"), Generated: "2026-08-21T11:00:00Z",
		Runs:              []Run{{StartedAt: "2026-08-21T10:03:00Z", Workflow: "scheduler", Conclusion: "success", ID: 1.0}},
		RunsFoldedThrough: "2026-08-21T10:03:00Z",
		QueueRecords:      []QueueRecord{closedRec("2026-08-20", "usage-fold", "done", "acme-pack-b")}, QueueFoldedThrough: "2026-08-20T22:00:00Z"})
	if first.Generated != "2026-08-21T11:00:00Z" || first.RunsFoldedThrough != "2026-08-21T10:03:00Z" || first.QueueFoldedThrough != "2026-08-20T22:00:00Z" {
		t.Fatalf("%+v", first)
	}
	if field(first.Weeks.ObjAt("2026-W34").ObjAt("queue").ObjAt("acme-pack-b/usage-fold"), "done") != 1.0 {
		t.Fatal("the day that closed carried its queue outcomes into its week")
	}
	second := foldOf(t, FoldIn{Prior: first, Today: "2026-08-21", Now: strp("2026-08-21T12:00:00Z"), Generated: "2026-08-21T12:00:00Z"})
	if second.RunsFoldedThrough != "2026-08-21T10:03:00Z" || second.QueueFoldedThrough != "2026-08-20T22:00:00Z" {
		t.Fatalf("%+v", second)
	}
	if !deepEqual(field(second.Days, "2026-08-20.queue"), field(first.Days, "2026-08-20.queue")) {
		t.Fatal(Stringify(second.Days))
	}
	if field(second.Weeks.ObjAt("2026-W34").ObjAt("queue").ObjAt("acme-pack-b/usage-fold"), "done") != 1.0 {
		t.Fatal("and the closed week is not folded a second time")
	}
}

func TestFoldUsageDropsDayRowsPastTheDayWindow(t *testing.T) {
	prior := UsageFile{Days: jsObj(t, `{"2026-06-01": {"captures": 1, "tasks": {}, "queue": {}}}`), Weeks: NewObj(), FoldedThrough: "2026-06-01"}
	folded := foldOf(t, FoldIn{Prior: prior, Today: "2026-08-21", Now: strp("2026-08-21T11:00:00Z")})
	if folded.Days.Has("2026-06-01") {
		t.Fatal(Stringify(folded.Days))
	}
}

// --- the retired census -----------------------------------------------------------

func TestCarryTaskRunsAgesTheSlotSchedulersRowsOutAndAppendsNothing(t *testing.T) {
	days := NewObj()
	CarryTaskRuns(days, jsObj(t, `{"2026-07-28": {"tasks": {"p/fresh": {"agent": 1}}}, "2026-06-01": {"tasks": {"p/ancient": {"agent": 9}}}}`), "2026-07-28")
	same(t, days.ObjAt("2026-07-28").ObjAt("tasks").ObjAt("p/fresh"), `{"agent": 1}`)
	if days.Has("2026-06-01") {
		t.Fatal(Stringify(days))
	}
}

func TestWithinTaskWindowKeepsTheLast14DaysAndNothingOlder(t *testing.T) {
	for date, in := range map[string]bool{"2026-07-29": true, "2026-07-16": true, "2026-07-15": false, "2026-07-30": false} {
		if WithinTaskWindow(date, "2026-07-29", TaskDayWindowDays) != in {
			t.Errorf("%s: want %v", date, in)
		}
	}
}

// --- executor execution statuses out of a captured session ----------------------

func execLine(status, slot string) string {
	return "claudinite-task-exec v1 tidy-repo/tidy-issues [" + slot + "] " + status
}

func emptyTaskExec(over string) *Obj { return spread(zeros(UsageFields["taskExec"]), litObj(over)) }

func TestCountTaskExecsReadsExecRecordsOutOfToolResultText(t *testing.T) {
	es := list(ObjOf("type", "user", "message", ObjOf("content", []any{ObjOf("type", "tool_result", "content",
		[]any{ObjOf("type", "text", "text", "brief...\n"+execLine("success", "d2026-08-06")+"\n")})})))
	if got, want := CountTaskExecs(es), ObjOf("tidy-repo/tidy-issues", emptyTaskExec(`{"success": 1}`)); !deepEqual(got, want) {
		t.Fatal(Stringify(got))
	}
}

func TestCountTaskExecsDedupesAnEchoedRecordAndKeepsDistinctStatusesAndSlots(t *testing.T) {
	es := list(
		ObjOf("type", "user", "message", ObjOf("content", []any{ObjOf("type", "tool_result", "content", execLine("failed", "d2026-08-06")+"\n")})),
		ObjOf("type", "assistant", "message", ObjOf("content", []any{ObjOf("type", "text", "text", "the run failed: "+execLine("failed", "d2026-08-06"))})),
		ObjOf("type", "user", "message", ObjOf("content", execLine("failed", "d2026-08-07"))),
	)
	if v := field(CountTaskExecs(es).ObjAt("tidy-repo/tidy-issues"), "failed"); v != 2.0 {
		t.Fatal(v)
	}
}

func TestCountEntriesCarriesTaskExecBesideTheOtherCounters(t *testing.T) {
	counts := CountEntries(list(ObjOf("type", "user", "message", ObjOf("content", execLine("task-gone", "d2026-08-06")))), Corpus{})
	if want := ObjOf("tidy-repo/tidy-issues", emptyTaskExec(`{"task-gone": 1}`)); !deepEqual(counts.TaskExec, want) {
		t.Fatal(Stringify(counts.TaskExec))
	}
}

// --- the file boundary ----------------------------------------------------------

func fieldIndex(file *Obj, group, name string) int {
	fields, _ := file.ObjAt("fields").Get(group)
	for i, f := range fields.([]any) {
		if f == name {
			return i
		}
	}
	return -1
}

func TestAFoldRoundTripsThroughTheFileUnchanged(t *testing.T) {
	folded := foldOf(t, FoldIn{Files: []CaptureFile{
		capture("2026-07-26", fp(1), "s1", `{"skillLoads": {"acme-skill-h": 2}, "checks": {"work": {"runs": 5, "failures": 1}}}`),
		capture("2026-07-27", fp(2), "s2", `{"skillLoads": {"acme-skill-i": 1}}`),
	}, Today: "2026-07-28"})
	reread := DecodeUsage(js(t, RenderUsageFile(EncodeUsage(folded))))
	if reread.FoldedThrough != folded.FoldedThrough {
		t.Fatal(reread.FoldedThrough)
	}
	same(t, field(reread.Days, "2026-07-26.skillLoads"), `{"acme-skill-h": 2}`)
	if field(reread.Days, "2026-07-26.checks.work.failures") != 1.0 || field(reread.Weeks, "2026-W30.captures") != 1.0 {
		t.Fatal(Stringify(reread.Days), Stringify(reread.Weeks))
	}
	again := foldOf(t, FoldIn{Prior: reread, Today: "2026-07-28"})
	if !deepEqual(again.Weeks, folded.Weeks) {
		t.Fatalf("%s\n%s", Stringify(again.Weeks), Stringify(folded.Weeks))
	}
}

func TestTheFileStatesTheVocabularyItsTuplesAreSpelledIn(t *testing.T) {
	file := EncodeUsage(foldOf(t, FoldIn{Files: []CaptureFile{capture("2026-07-26", fp(1), "s1", `{"checks": {"work": {"runs": 5, "failures": 1}}}`)}, Today: "2026-07-27"}))
	if v, _ := file.Get("version"); v != float64(UsageVersion) {
		t.Fatal(v)
	}
	if got, _ := file.ObjAt("fields").Get("checks"); !deepEqual(got, strList(UsageFields["checks"])) {
		t.Fatal(Stringify(got))
	}
	tuple, _ := path(file, "days", "2026-07-26", "checks", "work")
	if tuple.([]any)[fieldIndex(file, "checks", "runs")] != 5.0 || tuple.([]any)[fieldIndex(file, "checks", "failures")] != 1.0 {
		t.Fatal(Stringify(tuple))
	}
}

func TestAnEmptyCounterGroupIsOmittedFromTheFileNeverWrittenAsEmpty(t *testing.T) {
	file := EncodeUsage(foldOf(t, FoldIn{Files: []CaptureFile{capture("2026-07-26", fp(1), "s1", `{}`)}, Today: "2026-07-27"}))
	row := file.ObjAt("days").ObjAt("2026-07-26")
	if row.Has("tasks") || row.Has("taskExec") {
		t.Fatal(Stringify(row))
	}
	same(t, field(DecodeUsage(file).Days, "2026-07-26.tasks"), `{}`)
}

func TestAVersion1FileDecodesAsItself(t *testing.T) {
	decoded := DecodeUsage(js(t, `{"version": 1, "foldedThrough": "2026-07-26", "runsFoldedThrough": "2026-07-26T00:00:00Z", "days": {},
		"weeks": {"2026-W30": {"days": 3, "captures": 4, "merges": 4, "sessionDays": 3, "userMessages": 40, "userCommands": 1,
			"skillLoads": {"acme-skill-h": 3}, "checks": {"work": {"runs": 9, "failures": 2}}, "checkFindings": {}}}}`))
	if field(decoded.Weeks.ObjAt("2026-W30").ObjAt("skillLoads"), "acme-skill-h") != 3.0 {
		t.Fatal(Stringify(decoded.Weeks))
	}
	file := EncodeUsage(foldOf(t, FoldIn{Files: []CaptureFile{capture("2026-07-27", fp(1), "s1", `{}`)}, Prior: decoded, Today: "2026-07-28"}))
	if v, _ := file.Get("version"); v != float64(UsageVersion) {
		t.Fatal(v)
	}
	totals, _ := path(file, "weeks", "2026-W30", "totals")
	if totals.([]any)[fieldIndex(file, "week", "captures")] != 4.0 {
		t.Fatal(Stringify(totals))
	}
}

func TestTheFirstFoldAfterTheUpgradeRewritesTheWholeFileLosingNothing(t *testing.T) {
	v1 := jsObj(t, `{"version": 1, "foldedThrough": "2026-07-26", "runsFoldedThrough": "2026-07-26T03:00:00Z",
		"days": {"2026-07-26": {"captures": 2, "merges": 2, "sessions": 1, "userMessages": 9, "userCommands": 1,
			"skillLoads": {"acme-skill-h": 2},
			"checks": {"work": {"runs": 9, "failures": 2, "errors": 0, "blocking": 3, "advisory": 0, "ciRuns": 0, "ciFailures": 0}},
			"checkFindings": {"task-lifecycle": {"blocking": 3, "advisory": 0}},
			"tasks": {"tidy-repo/tidy-issues": {"agent": 1, "code-work": 0, "skipped": 5, "failed": 0, "deferred": 0}},
			"taskExec": {"tidy-repo/tidy-issues": {"success": 1, "failed": 0, "task-gone": 0, "invalid": 0}}}},
		"weeks": {"2026-W30": {"days": 3, "captures": 4, "merges": 4, "sessionDays": 3, "userMessages": 40, "userCommands": 1,
			"skillLoads": {"acme-skill-h": 3},
			"checks": {"work": {"runs": 20, "failures": 5, "errors": 0, "blocking": 8, "advisory": 0, "ciRuns": 0, "ciFailures": 0}},
			"checkFindings": {"task-lifecycle": {"blocking": 8, "advisory": 0}},
			"tasks": {"tidy-repo/tidy-issues": {"agent": 3, "code-work": 0, "skipped": 15, "failed": 0, "deferred": 0}},
			"taskExec": {"tidy-repo/tidy-issues": {"success": 3, "failed": 0, "task-gone": 0, "invalid": 0}}}}}`)
	written := EncodeUsage(foldOf(t, FoldIn{Prior: DecodeUsage(v1), Today: "2026-07-27"}))
	back := DecodeUsage(written)
	if v, _ := written.Get("version"); v != float64(UsageVersion) {
		t.Fatal(v)
	}
	if v, _ := written.Get("foldedThrough"); v != "2026-07-26" {
		t.Fatal("the day watermark does not move")
	}
	if v, _ := written.Get("runsFoldedThrough"); v != "2026-07-26T03:00:00Z" {
		t.Fatal("nor the run watermark")
	}
	if want := ObjOf("2026-W30", spread(emptyGroups(), v1.ObjAt("weeks").ObjAt("2026-W30"))); !deepEqual(back.Weeks, want) {
		t.Fatalf("got  %s\nwant %s", Stringify(back.Weeks), Stringify(want))
	}
	if !deepEqual(field(back.Days, "2026-07-26.tasks"), field(v1, "days.2026-07-26.tasks")) {
		t.Fatal(Stringify(back.Days))
	}
	if field(back.Days, "2026-07-26.captures") != 0.0 {
		t.Fatal("no capture file, no capture-derived count")
	}
	same(t, field(back.Days, "2026-07-26.taskExec"), `{}`)
}

func TestAddingAndRemovingAPackSkillOrTaskIsPureKeyPresence(t *testing.T) {
	encoded := func(loads string) *Obj {
		return EncodeUsage(foldOf(t, FoldIn{Files: []CaptureFile{capture("2026-07-26", fp(1), "s1", `{"skillLoads": `+loads+`}`)}, Today: "2026-07-27"}))
	}
	before, after := encoded(`{"old-skill": 2}`), encoded(`{"new-skill": 1}`)
	if k := before.ObjAt("days").ObjAt("2026-07-26").ObjAt("skillLoads").Keys(); !reflect.DeepEqual(k, []string{"old-skill"}) {
		t.Fatal(k)
	}
	if k := after.ObjAt("days").ObjAt("2026-07-26").ObjAt("skillLoads").Keys(); !reflect.DeepEqual(k, []string{"new-skill"}) {
		t.Fatal(k)
	}
	b, _ := path(before, "days", "2026-07-26", "totals")
	a, _ := path(after, "days", "2026-07-26", "totals")
	if !deepEqual(b, a) || !deepEqual(before.ObjAt("fields"), after.ObjAt("fields")) {
		t.Fatal("every other row's encoding is untouched")
	}
}

func TestACounterARowPredatesStaysUnknownInTheFileNeverAFabricatedZero(t *testing.T) {
	prior := UsageFile{FoldedThrough: "2026-07-26", Weeks: jsObj(t, `{"2026-W30": {"days": 2, "captures": 3, "merges": 3, "sessionDays": 2, "skillLoads": {}}}`), Days: NewObj()}
	file := EncodeUsage(foldOf(t, FoldIn{Prior: prior, Today: "2026-07-27"}))
	totals, _ := path(file, "weeks", "2026-W30", "totals")
	if totals.([]any)[fieldIndex(file, "week", "captures")] != 3.0 {
		t.Fatal(Stringify(totals))
	}
	if totals.([]any)[fieldIndex(file, "week", "userMessages")] != nil {
		t.Fatal(Stringify(totals))
	}
	if DecodeUsage(file).Weeks.ObjAt("2026-W30").Has("userMessages") {
		t.Fatal("a null slot decodes back to no key")
	}
}

// --- the mounted-skill set -----------------------------------------------------

func TestMountedSkillsReadsEachDeclaredPacksSkillsOffItsOwnMountAndRecordsNothingItCannotAsk(t *testing.T) {
	root := t.TempDir()
	skill := func(mount, pack, name string, withFile bool) {
		dir := root + "/" + mount + "/" + pack + "/skills/" + name
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if withFile {
			if err := os.WriteFile(dir+"/SKILL.md", []byte("# s\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	skill(".claudinite/shared/packs", "acme-pack", "acme-skill", true)
	skill(".claudinite/local/packs", "acme-local", "local-skill", true)
	skill(".claudinite/temp/packs", "acme-temp", "temp-skill", true)
	skill(".claudinite/shared/packs", "undeclared-pack", "never-mounted", true)
	skill(".claudinite/shared/packs", "acme-pack", "no-skill-file", false)
	mounted := MountedSkills([]string{
		root + "/.claudinite/shared/packs/acme-pack", root + "/.claudinite/local/packs/acme-local",
		root + "/.claudinite/temp/packs/acme-temp", root + "/.claudinite/shared/packs/acme-absent",
	})
	if !reflect.DeepEqual(mounted, map[string]bool{"acme-skill": true, "local-skill": true, "temp-skill": true}) {
		t.Fatal(mounted)
	}
	counted := CountEntries(list(litObj(`{"type": "user", "message": {"role": "user", "content": "x"}}`)), Corpus{Mounted: mounted})
	if counted.Moments.Len() != 0 || counted.SkillCaught.Len() != 0 {
		t.Fatal(Stringify(counted.Moments), Stringify(counted.SkillCaught))
	}
	if got := MountedSkills(nil); len(got) != 0 {
		t.Fatal(got)
	}
}
