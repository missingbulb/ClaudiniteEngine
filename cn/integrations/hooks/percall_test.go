package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
)

// fakeGuards answers every call with a fixed result, or sleeps or panics.
type fakeGuards struct {
	result GuardResult
	sleep  time.Duration
	panics bool
	calls  []Call
	// building is how long Prepare takes; prepared counts its calls.
	building time.Duration
	prepared int
}

func (f *fakeGuards) Prepare(repo, event string) ([]string, error) {
	f.prepared++
	time.Sleep(f.building)
	return []string{"[cn] buildwait " + event + " ok 1ms"}, nil
}

func (f *fakeGuards) Judge(repo string, call Call, deadline time.Time) GuardResult {
	f.calls = append(f.calls, call)
	if f.panics {
		panic("guard bug")
	}
	time.Sleep(f.sleep)
	return f.result
}

// perCall runs one per-call hook, returning stdout, stderr and the error.
func perCall(t *testing.T, h Handler, event, stdin string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := h.Run(event, strings.NewReader(stdin), &out, &errb, time.Now())
	return out.String(), errb.String(), err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func outcomeOf(t *testing.T, stderr string) string {
	t.Helper()
	m := crumb.FindStringSubmatch(lastLine(stderr))
	if m == nil {
		t.Fatalf("stderr does not end with a breadcrumb: %q", stderr)
	}
	return m[2]
}

func additional(t *testing.T, out, event string) string {
	t.Helper()
	if strings.TrimSpace(out) == "{}" {
		return ""
	}
	var got sessionStartOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if got.HookSpecificOutput.HookEventName != event {
		t.Errorf("hookEventName %q, want %q", got.HookSpecificOutput.HookEventName, event)
	}
	return got.HookSpecificOutput.AdditionalContext
}

const bashCall = `{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls","z":1,"a":2}}`

// A block on PreToolUse is exit 2 with the reason first on stderr and the
// breadcrumb last, nothing on stdout; the call the guards saw carries the
// input as sent, key order kept.
func TestABlockOnPreToolUseExitsTwo(t *testing.T) {
	g := &fakeGuards{result: GuardResult{Blocks: []string{"Blocked by a: no. Fix: yes", "Blocked by b: also"}}}
	out, errOut, err := perCall(t, Handler{Guards: g, ProjectDir: t.TempDir()}, "pre-tool-use", bashCall)
	if report.CodeOf(err) != report.Block {
		t.Fatalf("err %v", err)
	}
	if out != "" {
		t.Errorf("stdout %q", out)
	}
	if !strings.HasPrefix(errOut, "Blocked by a: no. Fix: yes\nBlocked by b: also\n") || outcomeOf(t, errOut) != "block" {
		t.Errorf("stderr %q", errOut)
	}
	if len(g.calls) != 1 || g.calls[0].Tool != "Bash" || string(g.calls[0].Input) != `{"command":"ls","z":1,"a":2}` || g.calls[0].Event != "pre-tool-use" {
		t.Errorf("calls %+v", g.calls)
	}
}

func TestAdviceIsContext(t *testing.T) {
	g := &fakeGuards{result: GuardResult{Advice: []string{"[claudinite a] careful"}, Notes: []string{"[cn] a note"}}}
	out, errOut, err := perCall(t, Handler{Guards: g, ProjectDir: t.TempDir()}, "pre-tool-use", bashCall)
	if err != nil {
		t.Fatal(err)
	}
	if got := additional(t, out, "PreToolUse"); got != "[claudinite a] careful" {
		t.Errorf("context %q", got)
	}
	if outcomeOf(t, errOut) != "advise" || !strings.Contains(errOut, "[cn] a note\n") {
		t.Errorf("stderr %q", errOut)
	}
}

// A block cannot block a prompt or a result: it is passed on as context,
// and the breadcrumb says the verdict was not what the guard wanted.
func TestABlockElsewhereBecomesContext(t *testing.T) {
	g := &fakeGuards{result: GuardResult{Blocks: []string{"Blocked by j: x"}, Advice: []string{"[claudinite k] y"}}}
	out, errOut, err := perCall(t, Handler{Guards: g, ProjectDir: t.TempDir()}, "post-tool-use", `{"tool_name":"Bash","tool_input":{},"tool_response":"r"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := additional(t, out, "PostToolUse"); got != "Blocked by j: x\n[claudinite k] y" {
		t.Errorf("context %q", got)
	}
	if outcomeOf(t, errOut) != "error" || !strings.Contains(errOut, "cannot block") {
		t.Errorf("stderr %q", errOut)
	}
	if string(g.calls[0].Response) != `"r"` {
		t.Errorf("response %q", g.calls[0].Response)
	}
}

// The checks build runs before the hook's deadline starts, so a call that
// waited for it is still judged, and the build's breadcrumb reaches stderr.
func TestTheBuildDoesNotSpendTheDeadline(t *testing.T) {
	t.Setenv("CLAUDINITE_HOOK_DEADLINE_MS", "100")
	g := &fakeGuards{result: GuardResult{Blocks: []string{"Blocked by j: x"}}, building: 300 * time.Millisecond}
	_, errOut, err := perCall(t, Handler{Guards: g, ProjectDir: t.TempDir()}, "pre-tool-use", bashCall)
	if report.CodeOf(err) != report.Block || g.prepared != 1 || len(g.calls) != 1 || !strings.Contains(errOut, "[cn] buildwait pre-tool-use ok 1ms\n") {
		t.Errorf("%v prepared %d judged %d %q", err, g.prepared, len(g.calls), errOut)
	}
}

func TestADeadlineLetsTheCallThrough(t *testing.T) {
	t.Setenv("CLAUDINITE_HOOK_DEADLINE_MS", "30")
	g := &fakeGuards{result: GuardResult{Blocks: []string{"Blocked by slow: x"}}, sleep: time.Second}
	start := time.Now()
	out, errOut, err := perCall(t, Handler{Guards: g, ProjectDir: t.TempDir()}, "pre-tool-use", bashCall)
	if err != nil || strings.TrimSpace(out) != "{}" || outcomeOf(t, errOut) != "deadline" || strings.Contains(errOut, "Blocked") {
		t.Errorf("%v %q %q", err, out, errOut)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("waited %v past a 30ms deadline", time.Since(start))
	}
}

func TestAHookThatCannotDecideLetsTheCallThrough(t *testing.T) {
	cases := []struct {
		name, event, stdin string
		g                  *fakeGuards
	}{
		{"not json", "pre-tool-use", `{not json`, &fakeGuards{result: GuardResult{Blocks: []string{"Blocked by a: x"}}}},
		{"no tool_name", "pre-tool-use", `{"tool_input":{}}`, &fakeGuards{result: GuardResult{Blocks: []string{"Blocked by a: x"}}}},
		{"no tool_name after", "post-tool-use", `{"tool_response":"x"}`, &fakeGuards{}},
		{"a guard panics", "pre-tool-use", bashCall, &fakeGuards{panics: true}},
	}
	for _, c := range cases {
		out, errOut, err := perCall(t, Handler{Guards: c.g, ProjectDir: t.TempDir()}, c.event, c.stdin)
		if err != nil || strings.TrimSpace(out) != "{}" || outcomeOf(t, errOut) != "error" {
			t.Errorf("%s: %v %q %q", c.name, err, out, errOut)
		}
	}
}

// triggered is a member declaring pack p, whose skill g forces itself on
// every moment.
func triggered(t *testing.T) string {
	t.Helper()
	return member(t, []string{"p"}, map[string]map[string]string{
		"p": {
			"pack.json": `{"version": "1.0", "minEngineVersion": "0.0.0"}`,
			"skills/g/SKILL.md": "---\nname: g\ndescription: d\nmetadata:\n  force-load-on-file-edits-paths:\n    - 'docs/{a,b}/**'\n" +
				"  force-load-on-tool-calls: ['Bash.command /\\bdeploy\\b/']\n  force-load-on-prompts-matching: ['/SHIP IT/']\n" +
				"  force-load-on-tool-results-matching: ['Bash /EGRESS/']\n  force-load-on-tool-calls-bad: x\n---\nbody\n",
		},
	})
}

// session writes a transcript recording the given assistant tool calls.
func session(t *testing.T, calls ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	var b strings.Builder
	for i, c := range calls {
		b.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t` + string(rune('0'+i)) + `",` + c + `}]}}` + "\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func payload(fields string, transcript string) string {
	if transcript != "" {
		fields += `,"transcript_path":` + strings.TrimSpace(mustJSON(transcript))
	}
	return "{" + fields + "}"
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestPreToolUseHoldsAnEditUntilTheSkillLoads(t *testing.T) {
	repo := triggered(t)
	h := Handler{Guards: &fakeGuards{}, ProjectDir: repo}
	edit := `"session_id":"s","tool_name":"Edit","tool_input":{"file_path":"` + filepath.Join(repo, "docs/b/x.md") + `"}`
	_, errOut, err := perCall(t, h, "pre-tool-use", payload(edit, session(t)))
	want := "Blocked: docs/b/x.md is edited only with the `g` skill loaded (the p pack's skill forces itself for docs/{a,b}/**). Load it first — Skill tool, skill: \"g\", or Read .claudinite/shared/packs/p/skills/g/SKILL.md — then retry the edit.\n"
	if report.CodeOf(err) != report.Block || !strings.HasPrefix(errOut, want) {
		t.Fatalf("%v\n%q\nwant prefix %q", err, errOut, want)
	}
	// A relative path resolves against the project; outside it is no path.
	rel := `"tool_name":"Write","tool_input":{"file_path":"docs/a/y"}`
	if _, _, err := perCall(t, h, "pre-tool-use", payload(rel, "")); report.CodeOf(err) != report.Block {
		t.Errorf("relative path: %v", err)
	}
	out := `"tool_name":"Write","tool_input":{"file_path":"/elsewhere/docs/a/y"}`
	if _, _, err := perCall(t, h, "pre-tool-use", payload(out, "")); err != nil {
		t.Errorf("outside the repo: %v", err)
	}
	// A transcript that is named but cannot be read loaded nothing.
	if _, _, err := perCall(t, h, "pre-tool-use", payload(edit, filepath.Join(t.TempDir(), "gone.jsonl"))); report.CodeOf(err) != report.Block {
		t.Errorf("an unreadable transcript cleared the hold: %v", err)
	}
	loaded := session(t, `"name":"Skill","input":{"skill":"g"}`)
	if o, e, err := perCall(t, h, "pre-tool-use", payload(edit, loaded)); err != nil || outcomeOf(t, e) != "ok" || strings.TrimSpace(o) != "{}" {
		t.Errorf("loaded: %v %q %q", err, o, e)
	}
	call := `"tool_name":"Bash","tool_input":{"command":"make deploy"}`
	_, errOut, err = perCall(t, h, "pre-tool-use", payload(call, ""))
	if report.CodeOf(err) != report.Block || !strings.HasPrefix(errOut, "Blocked: Bash is called only with the `g` skill loaded (the p pack's skill forces itself for Bash.command /\\bdeploy\\b/). Load it first — ") || !strings.Contains(errOut, "then retry the call.\n") {
		t.Errorf("call hold: %v %q", err, errOut)
	}
}

// A hold comes before the guards, which are not asked.
func TestAHoldComesBeforeTheGuards(t *testing.T) {
	repo := triggered(t)
	g := &fakeGuards{result: GuardResult{Blocks: []string{"Blocked by a: x"}}}
	_, errOut, _ := perCall(t, Handler{Guards: g, ProjectDir: repo}, "pre-tool-use", `{"tool_name":"Bash","tool_input":{"command":"deploy"}}`)
	if len(g.calls) != 0 || strings.Contains(errOut, "Blocked by a") {
		t.Errorf("guards asked %d times: %q", len(g.calls), errOut)
	}
}

func TestPromptNudgesOncePerSession(t *testing.T) {
	repo := triggered(t)
	h := Handler{Guards: &fakeGuards{}, ProjectDir: repo}
	out, errOut, err := perCall(t, h, "user-prompt-submit", payload(`"prompt":"please SHIP IT now"`, session(t)))
	want := "Claudinite: this prompt matches the `g` skill's trigger (/SHIP IT/, the p pack) — load it before acting on the prompt: Skill tool, skill: \"g\", or Read .claudinite/shared/packs/p/skills/g/SKILL.md."
	if err != nil || additional(t, out, "UserPromptSubmit") != want || outcomeOf(t, errOut) != "nudge" {
		t.Errorf("%v %q %q", err, out, errOut)
	}
	for _, p := range []string{"<task-notification>SHIP IT</task-notification>", "  [SYSTEM NOTIFICATION] SHIP IT", "   "} {
		if out, _, _ := perCall(t, h, "user-prompt-submit", payload(`"prompt":`+mustJSON(p), "")); strings.TrimSpace(out) != "{}" {
			t.Errorf("%q nudged: %q", p, out)
		}
	}
	loaded := session(t, `"name":"Read","input":{"file_path":"/x/skills/g/SKILL.md"}`)
	if out, _, _ := perCall(t, h, "user-prompt-submit", payload(`"prompt":"SHIP IT"`, loaded)); strings.TrimSpace(out) != "{}" {
		t.Errorf("loaded: %q", out)
	}
}

func TestResultNudges(t *testing.T) {
	repo := triggered(t)
	h := Handler{Guards: &fakeGuards{}, ProjectDir: repo}
	out, errOut, err := perCall(t, h, "post-tool-use", `{"tool_name":"Bash","tool_input":{"command":"curl"},"tool_response":{"stdout":"EGRESS denied"}}`)
	if err != nil || !strings.HasPrefix(additional(t, out, "PostToolUse"), "Claudinite: this Bash result matches the `g` skill's trigger (Bash /EGRESS/, the p pack) — load it before acting on the result: ") || outcomeOf(t, errOut) != "nudge" {
		t.Errorf("%v %q %q", err, out, errOut)
	}
	if out, _, _ := perCall(t, h, "post-tool-use", `{"tool_name":"Read","tool_input":{},"tool_response":"EGRESS"}`); strings.TrimSpace(out) != "{}" {
		t.Errorf("another tool: %q", out)
	}
}

// The malformed trigger entries are named at SessionStart; the skill's
// other triggers still bind.
func TestSessionStartNamesAMalformedTrigger(t *testing.T) {
	repo := member(t, []string{"p"}, map[string]map[string]string{
		"p": {"pack.json": `{"version": "1.0", "minEngineVersion": "0.0.0"}`,
			"skills/g/SKILL.md": "---\nname: g\nmetadata:\n  force-load-on-tool-calls: ['Bash /oops(/', 'WebFetch']\n---\n"},
	})
	out, _ := hook(t, Handler{ProjectDir: repo}, "session-start", startIn)
	if !strings.Contains(contextOf(t, out), "[cn] skill g (pack p): the force-load-on-tool-calls entry \"Bash /oops(/\" is not a trigger and binds nothing\n") {
		t.Errorf("%s", contextOf(t, out))
	}
	if _, _, err := perCall(t, Handler{ProjectDir: repo}, "pre-tool-use", `{"tool_name":"WebFetch","tool_input":{}}`); report.CodeOf(err) != report.Block {
		t.Errorf("the good trigger did not bind: %v", err)
	}
}

// Stop hands the transcript to the work run.
func TestStopPassesTheTranscript(t *testing.T) {
	repo := member(t, nil, nil)
	fc := &fakeChecks{}
	hook(t, Handler{Checks: fc, ProjectDir: repo}, "stop", `{"hook_event_name":"Stop","transcript_path":"/t/s.jsonl"}`)
	if len(fc.scopes) != 1 || fc.scopes[0].Transcript != "/t/s.jsonl" {
		t.Errorf("%+v", fc.scopes)
	}
}
