package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
)

func put(t *testing.T, root, rel, body string) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		if t == nil {
			panic(err)
		}
		t.Helper()
		t.Fatal(err)
	}
}

// member is a repo declaring ids; each id in held gets a vendored tree.
func member(t *testing.T, declared []string, held map[string]map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	block := ""
	if len(declared) > 0 {
		block = "packs:\n  declared:\n"
		for _, id := range declared {
			block += "    - " + id + "\n"
		}
	}
	put(t, repo, ".claudinite/settings.yaml", "engine:\n  version: \"1.1.0\"\n"+block)
	for id, files := range held {
		for rel, body := range files {
			put(t, repo, ".claudinite/shared/packs/"+id+"/"+rel, body)
		}
	}
	return repo
}

type fakeChecks struct {
	started []string
	// sessions is each Start's session id; startLine what Start answers.
	sessions  []string
	startLine string
	result    CheckResult
	ran       []string
	scopes    []RunScope
}

func (f *fakeChecks) Start(repo, session string) (string, error) {
	f.started = append(f.started, repo)
	f.sessions = append(f.sessions, session)
	return f.startLine, nil
}

func (f *fakeChecks) Run(repo, event string, scope RunScope, wait time.Duration) CheckResult {
	f.ran = append(f.ran, event+" "+strings.Join(scope.Tags, ",")+" "+wait.String())
	f.scopes = append(f.scopes, scope)
	return f.result
}

func hook(t *testing.T, h Handler, event, stdin string) (string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	if err := h.Run(event, strings.NewReader(stdin), &out, &errb, time.Now()); err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String()
}

func contextOf(t *testing.T, out string) string {
	t.Helper()
	var got sessionStartOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return got.HookSpecificOutput.AdditionalContext
}

const startIn = `{"session_id":"s","hook_event_name":"SessionStart","source":"startup"}`

var selfCheck = regexp.MustCompile(`(?m)^\[cn\] packs (\d+)/(\d+) loaded( \((.*)\))?$`)

type fakeIndex struct {
	wrote    []string
	imported bool
	empty    bool
}

func (f *fakeIndex) HasImport(string) bool { return f.imported }

func (f *fakeIndex) HasRules(string, string) bool { return !f.empty }

func (f *fakeIndex) Write(repo, engine string) (bool, error) {
	f.wrote = append(f.wrote, repo)
	return true, nil
}

// The context carries the engine's own lines only: the prose reaches the
// session through the rules index, which SessionStart writes when stale.
// Packs load in the Node registry's order, canon by name.
func TestSessionStartAssemblesDeclaredPacksInOrder(t *testing.T) {
	repo := member(t, []string{"zeta", "alpha", "gone"}, map[string]map[string]string{
		"zeta":  {"pack.json": `{"version": "2.0", "minEngineVersion": "0.0.0"}`, "RULES.md": "- zeta rule one\n- zeta rule two\n", "skills/z/SKILL.md": "z skill"},
		"alpha": {"pack.json": `{"version": "1.0", "minEngineVersion": "0.0.0"}`},
	})
	fc, fi := &fakeChecks{}, &fakeIndex{}
	out, _ := hook(t, Handler{Checks: fc, Index: fi, ProjectDir: repo}, "session-start", startIn)
	ctx := contextOf(t, out)
	if strings.Contains(ctx, "zeta rule one") || !strings.HasPrefix(ctx, "# Claudinite engine") {
		t.Fatalf("pack prose in the context:\n%s", ctx)
	}
	m := selfCheck.FindStringSubmatch(ctx)
	if m == nil || m[1] != "2" || m[2] != "3" || m[4] != "alpha 1.0: rules 0 skills 0; zeta 2.0: rules 2 skills 1; gone: not loaded" {
		t.Errorf("self-check %q in\n%s", m, ctx)
	}
	if !strings.Contains(ctx, "pack gone: not loaded: .claudinite/shared/packs/gone is missing") {
		t.Errorf("no not-loaded line:\n%s", ctx)
	}
	lines := strings.Split(strings.TrimRight(ctx, "\n"), "\n")
	if !crumb.MatchString(lines[len(lines)-1]) || !selfCheck.MatchString(lines[len(lines)-2]) {
		t.Errorf("the self-check line then the breadcrumb must end the context:\n%s", ctx)
	}
	if len(fc.started) != 1 || fc.started[0] != repo {
		t.Errorf("checks build started %v", fc.started)
	}
	if len(fi.wrote) != 1 || fi.wrote[0] != repo {
		t.Errorf("index written %v", fi.wrote)
	}
}

// A member whose CLAUDE.md lacks the import gets no pack rules at all, so
// SessionStart says so and names the line to add; with the import, or with
// an index that imports nothing (temp packs' prose is never imported), it
// says nothing, as verify does.
func TestSessionStartNamesAMissingClaudeMDImport(t *testing.T) {
	const missing = `[cn] rules not loaded: CLAUDE.md does not import .claudinite/cache/claudinite-rules.GENERATED.md; add the line "@.claudinite/cache/claudinite-rules.GENERATED.md"`
	withProse := member(t, []string{"zeta"}, map[string]map[string]string{
		"zeta": {"pack.json": `{"version": "1.0"}`, "RULES.md": "- zeta rule\n"},
	})
	noProse := member(t, []string{"alpha"}, map[string]map[string]string{
		"alpha": {"pack.json": `{"version": "1.0", "prose": null}`},
	})
	tempOnly := member(t, nil, nil)
	put(t, tempOnly, ".claudinite/temp/packs/current_user/RULES.md", "- mine\n")
	for _, c := range []struct {
		name     string
		repo     string
		imported bool
		empty    bool
		want     bool
	}{
		{"no import", withProse, false, false, true},
		{"imported", withProse, true, false, false},
		{"nothing to import", noProse, false, true, false},
		{"temp prose only", tempOnly, false, true, false},
	} {
		out, _ := hook(t, Handler{Index: &fakeIndex{imported: c.imported, empty: c.empty}, ProjectDir: c.repo}, "session-start", startIn)
		if got := strings.Contains(contextOf(t, out), missing); got != c.want {
			t.Errorf("%s: line present %v, want %v:\n%s", c.name, got, c.want, contextOf(t, out))
		}
	}
}

// A local pack declared as local/<name> and a temp pack present both load
// after the canon packs, and their skills mount after the canon's, so a
// shared name resolves to the canon pack's; a skill directory without a
// SKILL.md is never mounted.
func TestSessionStartLocalAndTempPacks(t *testing.T) {
	repo := member(t, []string{"canon", "local/mine"}, map[string]map[string]string{
		"canon": {"pack.json": `{"version": "1.0"}`, "skills/shared/SKILL.md": "---\nname: shared\nmetadata:\n  body: workflow\n---\nfrom canon"},
	})
	put(t, repo, ".claudinite/local/packs/mine/pack.json", `{"requires": ["absent"]}`)
	put(t, repo, ".claudinite/local/packs/mine/RULES.md", "- local rule\n")
	put(t, repo, ".claudinite/local/packs/mine/skills/shared/SKILL.md", "from local")
	put(t, repo, ".claudinite/local/packs/mine/skills/own/SKILL.md", "own")
	put(t, repo, ".claudinite/local/packs/mine/skills/checks-only/checks.mjs", "")
	put(t, repo, ".claudinite/temp/packs/current_user/pack.json", `{}`)
	put(t, repo, ".claudinite/temp/packs/current_user/skills/mine-too/SKILL.md", "temp")
	h := Handler{ProjectDir: repo}
	out, _ := hook(t, h, "session-start", startIn)
	ctx := contextOf(t, out)
	read := func(rel string) string { b, _ := os.ReadFile(filepath.Join(repo, rel)); return string(b) }
	if !strings.HasSuffix(read(".claude/skills/shared/SKILL.md"), "from canon") || read(".claude/skills/own/SKILL.md") != "own" || read(".claude/skills/mine-too/SKILL.md") != "temp" {
		t.Errorf("mounts wrong:\n%s", ctx)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude/skills/checks-only")); !os.IsNotExist(err) {
		t.Error("mounted a skill directory without SKILL.md")
	}
	m := selfCheck.FindStringSubmatch(ctx)
	if m == nil || m[4] != "canon 1.0: rules 0 skills 1; local/mine: rules 1 skills 2; temp/current_user: rules 0 skills 1" {
		t.Errorf("self-check %q in\n%s", m, ctx)
	}
	if !strings.Contains(ctx, "pack local/mine: requires absent, which is not declared;") {
		t.Errorf("no note for the undeclared requirement:\n%s", ctx)
	}
	if !strings.Contains(ctx, "pack local/mine: its JavaScript checks") {
		t.Errorf("no JavaScript-checks line for a skill's checks.mjs:\n%s", ctx)
	}
	a := assemble(repo, "0.0.0")
	if a.skills["shared"].Body != "workflow" || a.skills["shared"].Name != "shared" {
		t.Errorf("frontmatter %+v", a.skills["shared"])
	}
}

func TestSessionStartMountsSkills(t *testing.T) {
	repo := member(t, []string{"one", "two"}, map[string]map[string]string{
		"one": {"pack.json": `{"version": "1"}`, "skills/shared/SKILL.md": "from one", "skills/solo/SKILL.md": "solo"},
		"two": {"pack.json": `{"version": "1"}`, "skills/shared/SKILL.md": "from two"},
	})
	put(t, repo, ".claude/skills/mine/SKILL.md", "a person's own skill")
	put(t, repo, ".claude/skills/old/SKILL.md", "from a pack no longer declared")
	put(t, repo, ".claude/skills/old/.claudinite-mount", "dropped\n")
	h := Handler{ProjectDir: repo}
	out, _ := hook(t, h, "session-start", startIn)
	ctx := contextOf(t, out)
	read := func(rel string) string { b, _ := os.ReadFile(filepath.Join(repo, rel)); return string(b) }
	if read(".claude/skills/shared/SKILL.md") != "from one" || read(".claude/skills/solo/SKILL.md") != "solo" {
		t.Errorf("mounted: %q %q", read(".claude/skills/shared/SKILL.md"), read(".claude/skills/solo/SKILL.md"))
	}
	if !strings.Contains(ctx, "[cn] skill shared is offered by one and two; one's is mounted") {
		t.Errorf("no conflict line:\n%s", ctx)
	}
	if read(".claude/skills/mine/SKILL.md") != "a person's own skill" {
		t.Error("a person's skill was touched")
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude/skills/old")); !os.IsNotExist(err) {
		t.Error("the mount of an undeclared pack was not removed")
	}
	info, _ := os.Stat(filepath.Join(repo, ".claude/skills/solo/SKILL.md"))
	time.Sleep(20 * time.Millisecond)
	hook(t, h, "session-start", startIn)
	again, _ := os.Stat(filepath.Join(repo, ".claude/skills/solo/SKILL.md"))
	if !again.ModTime().Equal(info.ModTime()) {
		t.Error("an unchanged mount was rewritten")
	}
}

func TestSessionStartNamesJavaScriptItIgnores(t *testing.T) {
	repo := member(t, []string{"basics"}, map[string]map[string]string{
		"basics": {"pack.json": `{"version": "60928.1", "minEngineVersion": "1.60928.1"}`, "worldRules/x.mjs": "export default 1", "RULES.md": "- r\n",
			"tasks/release/task.json": `{"code_work": "node release.mjs"}`, "tasks/release/release.mjs": "", "tasks/release/preconditions.mjs": ""},
	})
	out, _ := hook(t, Handler{ProjectDir: repo}, "session-start", startIn)
	ctx := contextOf(t, out)
	if !strings.Contains(ctx, "pack basics: its JavaScript checks (worldRules/, workRules/, skills/*/checks.mjs) are not run by this engine; its declared checks are") {
		t.Errorf("%s", ctx)
	}
	if strings.Contains(ctx, "task") {
		t.Errorf("a task with a task.json runs, its .mjs included, yet the context names tasks:\n%s", ctx)
	}
}

// A task declared by a task.mjs alone is never discovered, so the note
// names it; one beside a task.json is.
func TestSessionStartNamesATaskDeclaredInJavaScript(t *testing.T) {
	repo := member(t, []string{"basics"}, map[string]map[string]string{
		"basics": {"pack.json": `{"version": "60928.1", "minEngineVersion": "1.60928.1"}`, "RULES.md": "- r\n",
			"tasks/old/task.mjs": "export default {}", "tasks/new/task.json": `{"code_work": "node w.mjs"}`, "tasks/new/task.mjs": "", "tasks/new/w.mjs": ""},
	})
	out, _ := hook(t, Handler{ProjectDir: repo}, "session-start", startIn)
	ctx := contextOf(t, out)
	if !strings.Contains(ctx, "pack basics: tasks/old is declared by a task.mjs, which this engine does not read, so it never runs; write its task.json") {
		t.Errorf("%s", ctx)
	}
	if strings.Contains(ctx, "tasks/new") || strings.Contains(ctx, "JavaScript checks") {
		t.Errorf("%s", ctx)
	}
}

func TestSessionStartRefusesAPackForANewerEngine(t *testing.T) {
	repo := member(t, []string{"future"}, map[string]map[string]string{
		"future": {"pack.json": `{"version": "1", "minEngineVersion": "99.991231.99"}`, "RULES.md": "- secret future rule\n"},
	})
	h := Handler{ProjectDir: repo, Engine: "1.1.0"}
	out, _ := hook(t, h, "session-start", startIn)
	ctx := contextOf(t, out)
	if strings.Contains(ctx, "secret future rule") || !strings.Contains(ctx, "pack future 1 needs engine 99.991231.99 or newer") {
		t.Errorf("%s", ctx)
	}
	if m := selfCheck.FindStringSubmatch(ctx); m == nil || m[1] != "0" || m[2] != "1" {
		t.Errorf("self-check %q", m)
	}
}

func TestSessionStartWithoutPacksStaysQuiet(t *testing.T) {
	repo := member(t, nil, nil)
	out, _ := hook(t, Handler{ProjectDir: repo}, "session-start", startIn)
	ctx := contextOf(t, out)
	if m := selfCheck.FindStringSubmatch(ctx); m == nil || m[1] != "0" || m[2] != "0" || m[3] != "" {
		t.Errorf("self-check %q in %s", m, ctx)
	}
}

func TestSessionStartIsFastWithAColdCache(t *testing.T) {
	repo := member(t, []string{"hello"}, map[string]map[string]string{
		"hello": {"pack.json": `{"version": "1.0"}`, "RULES.md": "- hi\n", "skills/hello/SKILL.md": "s", "checks/hello.go": "package checks\n"},
	})
	start := time.Now()
	hook(t, Handler{ProjectDir: repo, Checks: &fakeChecks{}}, "session-start", startIn)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("session-start took %v", d)
	}
}

func TestStopRunsWorkChecks(t *testing.T) {
	repo := member(t, nil, nil)
	blocking := CheckResult{Findings: []findings.Finding{{Class: findings.Coded, ID: "hello/hello-check", Path: "HELLO_FINDING", Sentence: "delete it"}}, Crumb: "[cn] checks stop ok 5ms"}
	fc := &fakeChecks{result: blocking}
	out, errOut := hook(t, Handler{Checks: fc, ProjectDir: repo}, "stop", `{"hook_event_name":"Stop","stop_hook_active":false}`)
	var got struct{ Decision, Reason string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Decision != "block" || !strings.Contains(got.Reason, "hello/hello-check HELLO_FINDING: delete it") {
		t.Errorf("%q %v", out, err)
	}
	if len(fc.ran) != 1 || fc.ran[0] != "stop work 30s" {
		t.Errorf("ran %v", fc.ran)
	}
	if !strings.Contains(errOut, "[cn] checks stop ok 5ms\n") || !strings.Contains(errOut, "[cn] hooks stop ok") {
		t.Errorf("stderr %q", errOut)
	}
	// Claude Code is already continuing because of a stop hook: say it on
	// stderr, never block a second time in a row.
	out, _ = hook(t, Handler{Checks: fc, ProjectDir: repo}, "stop", `{"hook_event_name":"Stop","stop_hook_active":true}`)
	if strings.TrimSpace(out) != "{}" {
		t.Errorf("re-entry: %q", out)
	}
	fc.result = CheckResult{Findings: []findings.Finding{{Class: findings.Advisory, ID: "a/b", Path: ".", Sentence: "fyi"}}, Crumb: "[cn] checks stop ok 1ms"}
	if out, _ := hook(t, Handler{Checks: fc, ProjectDir: repo}, "stop", `{}`); strings.TrimSpace(out) != "{}" {
		t.Errorf("an advisory blocked: %q", out)
	}
	fc.result = CheckResult{Crumb: "[cn] checks stop timeout 30000ms", Err: errTest}
	out, errOut = hook(t, Handler{Checks: fc, ProjectDir: repo}, "stop", `{}`)
	if strings.TrimSpace(out) != "{}" || !strings.Contains(errOut, "[cn] checks stop timeout") {
		t.Errorf("timeout: %q %q", out, errOut)
	}
}

var errTest = os.ErrDeadlineExceeded

// The check build's own breadcrumbs reach the transcript: SessionStart's
// in its context, a Stop's on stderr beside the checks crumb, both with
// the session's id handed to the checks.
func TestTheCheckBuildReportsThroughTheHooks(t *testing.T) {
	repo := member(t, nil, nil)
	fc := &fakeChecks{startLine: "[cn] build started ok 3ms", result: CheckResult{Crumb: "[cn] buildwait stop ok 4200ms\n[cn] build compiled ok 5712ms\n[cn] checks stop ok 4300ms"}}
	out, _ := hook(t, Handler{Checks: fc, ProjectDir: repo}, "session-start", `{"session_id":"s9","hook_event_name":"SessionStart","source":"startup"}`)
	if ctx := contextOf(t, out); !strings.Contains(ctx, "[cn] build started ok 3ms\n") {
		t.Errorf("context:\n%s", ctx)
	}
	if len(fc.sessions) != 1 || fc.sessions[0] != "s9" {
		t.Errorf("start sessions %v", fc.sessions)
	}
	_, errOut := hook(t, Handler{Checks: fc, ProjectDir: repo}, "stop", `{"session_id":"s9","hook_event_name":"Stop"}`)
	if !strings.Contains(errOut, "[cn] buildwait stop ok 4200ms\n[cn] build compiled ok 5712ms\n") {
		t.Errorf("stderr %q", errOut)
	}
	if len(fc.scopes) != 1 || fc.scopes[0].Session != "s9" {
		t.Errorf("stop scope %+v", fc.scopes)
	}
}

// A pending adoption question is one engine line.
func TestSessionStartNamesPendingQuestions(t *testing.T) {
	asks := map[string]string{"pack.json": `{"version": "1.0", "questions": [{"id": "goals", "prompt": "Why?"}]}`}
	repo := member(t, []string{"asks"}, map[string]map[string]string{"asks": asks})
	out, _ := hook(t, Handler{ProjectDir: repo}, "session-start", startIn)
	want := "[cn] adoption questions pending (asks/goals): in an interactive session, at a natural moment, ask the person and record each with cn adopt --answer <pack>/<question>=<answer>; an unattended session ignores this\n"
	if ctx := contextOf(t, out); !strings.Contains(ctx, want) {
		t.Errorf("no pending line:\n%s", ctx)
	}
}

type fakeUserPack struct{ calls []string }

func (f *fakeUserPack) Prepare(repo string) string {
	f.calls = append(f.calls, repo)
	put(nil, repo, ".claudinite/temp/packs/current_user/pack.json", `{}`)
	put(nil, repo, ".claudinite/temp/packs/current_user/skills/mine/SKILL.md", "copied")
	return "[cn] personal pack: copied preferences/ariel/ from acme/store for GitHub user ariel."
}

// The user-pack step runs before the pack set is read, so the pack it
// copies in loads and mounts in the same session, and its line leads the
// context after the hello rule.
func TestSessionStartCopiesThePersonsPackBeforeReadingThePacks(t *testing.T) {
	repo := member(t, []string{"canon"}, map[string]map[string]string{"canon": {"pack.json": `{"version": "1.0"}`}})
	up := &fakeUserPack{}
	out, _ := hook(t, Handler{ProjectDir: repo, UserPack: up}, "session-start", startIn)
	ctx := contextOf(t, out)
	if len(up.calls) != 1 || up.calls[0] != repo {
		t.Fatalf("Prepare calls %v", up.calls)
	}
	step := strings.Index(ctx, "\n[cn] personal pack: copied preferences/ariel/ from acme/store for GitHub user ariel.\n")
	if step < strings.Index(ctx, HelloRule()) || step > strings.Index(ctx, "[cn] packs ") {
		t.Errorf("the step's line follows the hello rule and leads the packs:\n%s", ctx)
	}
	if m := selfCheck.FindStringSubmatch(ctx); m == nil || !strings.Contains(m[4], "temp/current_user: rules 0 skills 1") {
		t.Errorf("the copied pack loads in the same session: %q", m)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".claude/skills/mine/SKILL.md")); string(b) != "copied" {
		t.Error("the copied pack's skill is not mounted")
	}
}
