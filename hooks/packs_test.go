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

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
)

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
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
	result  CheckResult
	ran     []string
}

func (f *fakeChecks) Start(repo string) error { f.started = append(f.started, repo); return nil }

func (f *fakeChecks) Run(repo, event string, tags []string, wait time.Duration) CheckResult {
	f.ran = append(f.ran, event+" "+strings.Join(tags, ",")+" "+wait.String())
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

type fakeIndex struct{ wrote []string }

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
	if !strings.Contains(ctx, "pack local/mine: its coded checks") {
		t.Errorf("no coded-checks line for a skill's checks.mjs:\n%s", ctx)
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
		"basics": {"pack.json": `{"version": "60928.1", "minEngineVersion": "60928.1"}`, "worldRules/x.mjs": "export default 1", "RULES.md": "- r\n"},
	})
	out, _ := hook(t, Handler{ProjectDir: repo}, "session-start", startIn)
	if ctx := contextOf(t, out); !strings.Contains(ctx, "pack basics: its coded checks (worldRules/, workRules/, skills/*/checks.mjs) and tasks are not run by this engine; its declared checks are") {
		t.Errorf("%s", ctx)
	}
}

func TestSessionStartRefusesAPackForANewerEngine(t *testing.T) {
	repo := member(t, []string{"future"}, map[string]map[string]string{
		"future": {"pack.json": `{"version": "1", "minEngineVersion": "99999.0.0"}`, "RULES.md": "- secret future rule\n"},
	})
	h := Handler{ProjectDir: repo, Engine: "1.1.0"}
	out, _ := hook(t, h, "session-start", startIn)
	ctx := contextOf(t, out)
	if strings.Contains(ctx, "secret future rule") || !strings.Contains(ctx, "pack future 1 needs engine 99999.0.0 or newer") {
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
