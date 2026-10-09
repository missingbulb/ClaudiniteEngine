package usage

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The entry shapes are the real ones: a hook's stderr reaches the
// transcript as a meta user turn, and every load is a tool_use block.

func hookTurn(lines ...string) *Obj {
	return ObjOf("type", "user", "isMeta", true, "message", ObjOf("content", strings.Join(lines, "\n")))
}

func assistant(blocks ...*Obj) *Obj {
	content := make([]any, len(blocks))
	for i, b := range blocks {
		content[i] = b
	}
	return ObjOf("type", "assistant", "message", ObjOf("content", content))
}

func skillBlock(name string) *Obj {
	return ObjOf("type", "tool_use", "name", "Skill", "input", ObjOf("skill", name), "id", "t"+name)
}

func callBlock(name string, input *Obj) *Obj {
	return ObjOf("type", "tool_use", "name", name, "input", input, "id", "t"+name)
}

func human(text string) *Obj {
	return ObjOf("type", "user", "origin", ObjOf("kind", "human"), "message", ObjOf("content", text))
}

func hookLine(hook, message, at, run string) string {
	return at + " run=" + run + " " + hook + ": " + message
}

func entries(es ...*Obj) []any {
	out := make([]any, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out
}

func causes(t *testing.T, over string) *Obj { return spread(zeros(LoadCauses), jsObj(t, over)) }

func renderTiming(scope, total string, rules ...string) string {
	return strings.Join(append([]string{"claudinite-check-timing v1 " + scope + " total=" + total}, rules...), " ")
}

func TestHookMarksReadTheLogLinesOutOfWhateverEntryCarriedThemOnceEach(t *testing.T) {
	line := hookLine("Stop", "done exit=0 checks-passed", "2026-09-21T10:00:00Z", "1")
	marks := HookMarks(hookTurn("Stop hook feedback:", line), map[string]bool{})
	if !reflect.DeepEqual(marks, []HookMark{{Stamp: "2026-09-21T10:00:00Z", Run: "1", Hook: "Stop", Message: "done exit=0 checks-passed"}}) {
		t.Fatalf("%+v", marks)
	}
	seen := map[string]bool{}
	if n := len(HookMarks(hookTurn(line), seen)); n != 1 {
		t.Fatal(n)
	}
	if n := len(HookMarks(hookTurn(line), seen)); n != 0 {
		t.Fatal("the second recording of one emission is not a second mark")
	}
	if n := len(HookMarks(hookTurn(hookLine("Stop", "done exit=0 checks-passed", "2026-09-21T10:00:00Z", "2")), seen)); n != 1 {
		t.Fatal("two real emissions differ by their run id at least")
	}
}

func TestReadMarkClassifiesTheFourMarksAGuardLeavesAndNothingElse(t *testing.T) {
	cases := []struct {
		hook, message string
		want          Mark
	}{
		{"PreToolUse", "done exit=2 skill-not-loaded packs/a/RULES.md needs acme-skill-b,acme-skill-c", Mark{Kind: "block", Cause: "blockedEdit", Skills: []string{"acme-skill-b", "acme-skill-c"}}},
		{"PreToolUse", "done exit=2 skill-not-loaded-for-call Bash needs acme-skill", Mark{Kind: "block", Cause: "blockedCall", Skills: []string{"acme-skill"}}},
		{"PreToolUse", "done exit=2 action-guard no-pr-polling,ask-already-decided", Mark{Kind: "guard", Severity: "blocking", Rules: []string{"no-pr-polling", "ask-already-decided"}}},
		{"PreToolUse", "advisory action-guard acme-check", Mark{Kind: "guard", Severity: "advisory", Rules: []string{"acme-check"}}},
		{"PostToolUse", "skill-trigger WebFetch acme-skill-d", Mark{Kind: "trigger", Cause: "resultTrigger", Skills: []string{"acme-skill-d"}}},
		{"UserPromptSubmit", "skill-trigger acme-skill-e", Mark{Kind: "trigger", Cause: "promptTrigger", Skills: []string{"acme-skill-e"}}},
	}
	for _, c := range cases {
		if got, ok := ReadMark(HookMark{Hook: c.hook, Message: c.message}); !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v", c.message, got)
		}
	}
	for _, not := range []HookMark{
		{Hook: "PreToolUse", Message: "done exit=0 allowed"},
		{Hook: "Stop", Message: "done exit=0 checks-passed"},
		{Hook: "PreToolUse", Message: "action-guard-failed some-rule boom"},
	} {
		if _, ok := ReadMark(not); ok {
			t.Errorf("%s is not a corpus mark", not.Message)
		}
	}
}

func TestALoadIsAttributedToTheMarkThatCausedItAndToNothingOnceThatMarkIsSpent(t *testing.T) {
	use := CountCorpusUse(entries(
		hookTurn(hookLine("PreToolUse", "done exit=2 skill-not-loaded-for-call Bash needs acme-skill", "2026-09-21T10:00:00Z", "1")),
		assistant(skillBlock("acme-skill")),
		assistant(skillBlock("acme-skill")),
	), nil)
	same(t, field(use.SkillLoadsBy, "acme-skill"), Stringify(causes(t, `{"blockedCall": 1, "voluntary": 1}`)))
	same(t, use.SkillBlocks, `{"acme-skill": 1}`)
}

func TestALoadNothingPrecededIsVoluntaryAndTheWayTheBodyArrivedNamesItself(t *testing.T) {
	use := CountCorpusUse(entries(
		assistant(skillBlock("acme-skill-f")),
		assistant(callBlock("Read", ObjOf("file_path", "packs/acme-pack/skills/acme-skill-g/SKILL.md"))),
		ObjOf("type", "user", "message", ObjOf("content", "<command-name>/acme-skill-h</command-name>")),
		assistant(callBlock("Read", ObjOf("file_path", "docs/acme-doc/DESIGN.md"))),
	), map[string]bool{"acme-skill-f": true, "acme-skill-g": true, "acme-skill-h": true})
	same(t, field(use.SkillLoadsBy, "acme-skill-f"), Stringify(causes(t, `{"voluntary": 1}`)))
	same(t, field(use.SkillLoadsBy, "acme-skill-g"), Stringify(causes(t, `{"read": 1}`)))
	same(t, field(use.SkillLoadsBy, "acme-skill-h"), Stringify(causes(t, `{"command": 1}`)))
	if use.SkillLoadsBy.Len() != 3 {
		t.Fatal("a Read of something that is not a mounted skill is not a load")
	}
}

func TestATriggerThatFiredIsFollowedOnlyByALoadAfterIt(t *testing.T) {
	fire := hookTurn(hookLine("PostToolUse", "skill-trigger WebFetch acme-skill-d", "2026-09-21T10:00:00Z", "1"))
	same(t, field(CountCorpusUse(entries(fire, assistant(skillBlock("acme-skill-d"))), nil).TriggerFires, "acme-skill-d"), `{"fired": 1, "followed": 1}`)
	same(t, field(CountCorpusUse(entries(fire, assistant(callBlock("Bash", ObjOf("command", "echo on with the work")))), nil).TriggerFires, "acme-skill-d"),
		`{"fired": 1, "followed": 0}`)
	same(t, field(CountCorpusUse(entries(assistant(skillBlock("acme-skill-d")), fire), nil).TriggerFires, "acme-skill-d"), `{"fired": 1, "followed": 0}`)
}

func TestGuardFiringsAreCountedPerRuleByWhatTheCallWasTold(t *testing.T) {
	use := CountCorpusUse(entries(
		hookTurn(hookLine("PreToolUse", "advisory action-guard acme-check", "2026-09-21T10:00:00Z", "1")),
		hookTurn(hookLine("PreToolUse", "advisory action-guard acme-check", "2026-09-21T10:00:01Z", "2")),
		hookTurn(hookLine("PreToolUse", "done exit=2 action-guard no-pr-polling", "2026-09-21T10:00:02Z", "3")),
	), nil)
	same(t, use.GuardFires, `{"acme-check": {"blocking": 0, "advisory": 2}, "no-pr-polling": {"blocking": 1, "advisory": 0}}`)
}

func TestCountToolCallsCountsEveryCallASubagentsIncludedAndNamesNoSkill(t *testing.T) {
	same(t, CountToolCalls(entries(
		assistant(callBlock("Bash", ObjOf("command", "ls")), callBlock("Bash", ObjOf("command", "pwd"))),
		assistant(callBlock("Read", ObjOf("file_path", "a.mjs"))),
		human("not a call"),
	)), `{"Bash": 2, "Read": 1}`)
}

// The engine's trigger predicates, stood in for over the shapes the
// counters read.
var standInHits = Hits{
	Call: func(d Declaration, c ToolCall) bool {
		dd := d.Data.(map[string]any)
		v, _ := propOf(c.Input, dd["field"].(string))
		s := ""
		if v != nil {
			s = jsString(v)
		}
		return d.Kind == "toolCall" && dd["tool"] == c.Name && dd["pattern"].(*regexp.Regexp).MatchString(s)
	},
	Prompt: func(d Declaration, text string) bool {
		return d.Kind == "prompt" && d.Data.(map[string]any)["pattern"].(*regexp.Regexp).MatchString(text)
	},
	Path: func(d Declaration, p string) bool {
		return d.Data.(map[string]any)["re"].(*regexp.Regexp).MatchString(p)
	},
}

var gitCommitDecl = Declaration{Skill: "acme-skill", Kind: "toolCall", Data: map[string]any{"tool": "Bash", "field": "command", "pattern": regexp.MustCompile(`git commit`)}}

func TestMomentsCountEveryOccasionADeclarationNamedLoadedOrNot(t *testing.T) {
	declarations := []Declaration{
		gitCommitDecl,
		{Skill: "acme-skill-e", Kind: "prompt", Data: map[string]any{"pattern": regexp.MustCompile(`/acme-skill-e`)}},
		{Skill: "acme-skill-i", Path: true, Data: map[string]any{"re": regexp.MustCompile(`^(?:.*/)?[^/]*\.test\.mjs$`)}},
	}
	got := CountMoments(entries(
		assistant(callBlock("Bash", ObjOf("command", "git commit -m one"))),
		assistant(skillBlock("acme-skill")),
		assistant(callBlock("Bash", ObjOf("command", "git commit -m two"))),
		human("please /acme-skill-e this"),
		assistant(callBlock("Edit", ObjOf("file_path", "engine-tests/a.test.mjs"))),
		assistant(callBlock("Edit", ObjOf("file_path", "engine/a.mjs"))),
	), declarations, standInHits)
	same(t, got, `{"acme-skill": 2, "acme-skill-e": 1, "acme-skill-i": 1}`)
}

func TestMomentsRecordNoKeyWhereTheEngineCouldNotResolveTheDeclarations(t *testing.T) {
	es := entries(assistant(callBlock("Bash", ObjOf("command", "git commit -m one"))))
	same(t, CountMoments(es, []Declaration{gitCommitDecl}, Hits{}), `{}`)
	same(t, CountMoments(es, []Declaration{gitCommitDecl}, Hits{Call: standInHits.Call}), `{}`)
}

func TestTheTimingReaderReadsTheV1TimingLine(t *testing.T) {
	got, ok := ParseTiming(renderTiming("work", "1175", "acme-check-b=928", "acme-check-c=132"))
	want := Timing{Scope: "work", TotalMs: 1175, Rules: []RuleTiming{{"acme-check-b", 928}, {"acme-check-c", 132}}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if _, ok := ParseTiming("0 blocking, 1 advisory (work scope: all)."); ok {
		t.Fatal("a summary line is not a timing line")
	}
}

func TestCheckTimingKeysTheSweepAndItsNamedRulesApartAndAPeakIsNotASum(t *testing.T) {
	timing := CountCheckTiming(entries(
		hookTurn(hookLine("Stop", renderTiming("work", "100", "a-rule=60"), "2026-09-21T10:00:00Z", "1")),
		hookTurn(hookLine("Stop", renderTiming("work", "300", "a-rule=40"), "2026-09-21T10:00:01Z", "2")),
	))
	same(t, field(timing, "work"), `{"runs": 2, "totalMs": 400, "maxMs": 300}`)
	same(t, timing.ObjAt("work/a-rule"), `{"runs": 2, "totalMs": 100, "maxMs": 60}`)
}
