package update

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const (
	v1   = "60930.1.0"
	v2   = "60930.2.0"
	v3   = "60930.3.0"
	pin1 = "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
)

// cnScript is a stand-in binary: selftest reports ver, verify runs
// verifyBody, workflows diff prints diffBody.
func cnScript(ver, verifyBody, diffBody string) []byte {
	return []byte("#!/bin/sh\ncase \"$1\" in\n" +
		"selftest) echo \"version " + ver + "\"; echo \"platform test\" ;;\n" +
		"verify) " + verifyBody + " ;;\n" +
		"workflows) printf '%s' '" + diffBody + "' ;;\n" +
		"esac\n")
}

func settingsFor(f settings.Format, ver, pin string) string {
	switch f {
	case settings.TOML:
		return "# kept\n[engine]\npackage = \"" + pkg + "\"\nversion = \"" + ver + "\"\nmanifest = \"" + pin + "\"\n"
	case settings.JSON:
		return "{\n  \"engine\": {\"package\": \"" + pkg + "\", \"version\": \"" + ver + "\", \"manifest\": \"" + pin + "\"},\n  \"other\": 1\n}\n"
	}
	return "# kept\nengine:\n  package: \"" + pkg + "\"\n  version: \"" + ver + "\"\n  manifest: \"" + pin + "\"\n"
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type world struct {
	reg  *registry
	hub  *fakeGitHub
	repo string
	bare string
	out  *bytes.Buffer
	f    settings.Format
	// key is the Actions key the run gets; nil is an ok organization key.
	key *KeyResult
	// keyCalls counts the run's key requests.
	keyCalls int
}

func newWorld(t *testing.T, f settings.Format) *world {
	t.Helper()
	w := &world{reg: newRegistry(t), hub: newFake(), out: &bytes.Buffer{}, f: f}
	root := t.TempDir()
	w.bare, w.repo = filepath.Join(root, "origin.git"), filepath.Join(root, "member")
	gitRun(t, root, "init", "-q", "--bare", "-b", "main", w.bare)
	gitRun(t, root, "init", "-q", "-b", "main", w.repo)
	_ = os.MkdirAll(filepath.Join(w.repo, ".claudinite"), 0o755)
	if err := os.WriteFile(filepath.Join(w.repo, settings.RelPath(f)), []byte(settingsFor(f, v1, pin1)), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, w.repo, "add", "-A")
	gitRun(t, w.repo, "commit", "-q", "-m", "adopt")
	gitRun(t, w.repo, "remote", "add", "origin", w.bare)
	gitRun(t, w.repo, "push", "-q", "origin", "main")
	w.mainRun(t, "success")
	return w
}

func (w *world) head(t *testing.T) string { return gitRun(t, w.repo, "rev-parse", "HEAD") }

// mainRun sets main's CI: a conclusion, in_progress, or none.
func (w *world) mainRun(t *testing.T, state string) {
	sha := w.head(t)
	w.hub.runs[sha] = nil
	if state == "none" {
		return
	}
	r := githubapi.Run{HeadSHA: sha, Event: "push", Status: "completed", Conclusion: state, CreatedAt: "2026-10-01T00:00:00Z"}
	if state == "in_progress" {
		r.Status, r.Conclusion = "in_progress", ""
	}
	w.hub.runs[sha] = []githubapi.Run{r}
}

func (w *world) deps(t *testing.T) Deps {
	return Deps{GitHub: w.hub, Registry: w.reg.client(), Git: gitcmd.Repo{Dir: w.repo}, Roots: rootsOf(testRoot),
		CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Platform: version.Platform(), Now: func() time.Time { return t0 },
		Repo: w.repo, Out: w.out, Timeout: 10 * time.Second, Key: func() KeyResult {
			w.keyCalls++
			if w.key != nil {
				return *w.key
			}
			return KeyResult{Key: &LicenseKey{Plan: "organization", State: "ok", IssuedAt: t0}}
		}}
}

func (w *world) publish(t *testing.T, ver string, o relOpts) {
	if o.binary == nil {
		o.binary = cnScript(ver, "exit 0", "")
	}
	w.reg.publish(t, pkg, ver, o)
}

func TestRedMainSkipsBeforeAnyNpmRead(t *testing.T) {
	for state, reason := range map[string]string{"failure": "failure", "cancelled": "cancelled", "in_progress": "in_progress", "none": "no run"} {
		w := newWorld(t, settings.YAML)
		w.publish(t, v2, relOpts{})
		w.mainRun(t, state)
		want := "skipped: main is not green (" + reason + ")"
		v, err := Engine(w.deps(t), Options{})
		if err != nil || v != want {
			t.Errorf("%s: %q %v, want %q", state, v, err, want)
		}
		if reqs := w.reg.requests(); len(reqs) != 0 {
			t.Errorf("%s: npm read before the red-main guard: %v", state, reqs)
		}
		if len(w.hub.called("create-pull")) != 0 {
			t.Errorf("%s: opened a PR", state)
		}
	}
}

// A gated pull_request run the job token's PR left behind is not CI.
func TestRedMainIgnoresActionRequiredRuns(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha := w.head(t)
	w.hub.runs[sha] = append([]githubapi.Run{{HeadSHA: sha, Event: "pull_request", Status: "completed", Conclusion: "action_required", CreatedAt: "2026-10-01T01:00:00Z"}}, w.hub.runs[sha]...)
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #1 for "+v2 {
		t.Errorf("%q %v\n%s", v, err, w.out)
	}
}

func TestProposeOpensAPinOnlyPR(t *testing.T) {
	for _, f := range settings.Formats {
		w := newWorld(t, f)
		w.publish(t, v2, relOpts{})
		v, err := Engine(w.deps(t), Options{})
		if err != nil || v != "opened #1 for "+v2 {
			t.Fatalf("%s: %q %v\n%s", f, v, err, w.out)
		}
		branch := "claudinite/engine-" + v2
		if got := gitRun(t, w.bare, "rev-list", "--count", "main.."+branch); got != "1" {
			t.Errorf("%s: %s commits on the branch", f, got)
		}
		if got := gitRun(t, w.bare, "diff", "--name-only", "main", branch); got != settings.RelPath(f) {
			t.Errorf("%s: branch changes %q", f, got)
		}
		if got := gitRun(t, w.bare, "log", "-1", "--format=%an <%ae>", branch); got != gitcmd.BotName+" <"+gitcmd.BotEmail+">" {
			t.Errorf("%s: committed as %s", f, got)
		}
		committed := gitRun(t, w.bare, "show", branch+":"+settings.RelPath(f)) + "\n"
		e, err := settings.ReadEngine([]byte(committed), f)
		if err != nil || e.Version != v2 {
			t.Errorf("%s: committed settings %+v %v", f, e, err)
		}
		want, _ := settings.SetPin([]byte(settingsFor(f, v1, pin1)), f, v2, e.Manifest)
		if committed != string(want) {
			t.Errorf("%s: not a value edit:\n%s\nwant\n%s", f, committed, want)
		}
		if got := gitRun(t, w.repo, "symbolic-ref", "--short", "HEAD"); got != "main" {
			t.Errorf("%s: left the checkout on %s", f, got)
		}
		creates := w.hub.called("create-pull")
		if len(creates) != 1 || !strings.Contains(creates[0], "Claudinite engine "+v2+"|"+branch+"|main|") {
			t.Fatalf("%s: %v", f, creates)
		}
		body := creates[0]
		for _, s := range []string{v2, e.Manifest, "version " + v2, "Key"} {
			if !strings.Contains(body, s) {
				t.Errorf("%s: PR body lacks %q:\n%s", f, s, body)
			}
		}
		if strings.Contains(body, "--force") {
			t.Errorf("%s: PR body mentions --force", f)
		}
		if got := w.hub.called("label"); len(got) != 1 || got[0] != "label 1 claudinite-update" {
			t.Errorf("%s: labels %v", f, got)
		}
		if got := w.hub.called("dispatch"); len(got) != 1 || got[0] != "dispatch claudinite-ci.yml "+branch+" pr=1" {
			t.Errorf("%s: dispatches %v", f, got)
		}
		if _, err := os.Stat(filepath.Join(w.deps(t).CacheRoot, v2)); err == nil {
			t.Errorf("%s: deps() cache reused", f)
		}
	}
}

func TestVerifyBreakOpensNoPR(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v2, `echo "break rehearsal .: this repo is deliberately broken"; exit 1`, "")})
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "no PR: "+v2+" would break this repo" {
		t.Fatalf("%q %v", v, err)
	}
	if !strings.Contains(w.out.String(), "break rehearsal .: this repo is deliberately broken") {
		t.Errorf("findings not printed:\n%s", w.out)
	}
	if out := gitRun(t, w.bare, "branch", "--list", "claudinite/*"); out != "" {
		t.Errorf("branch pushed: %s", out)
	}
	if len(w.hub.called("create-pull")) != 0 || len(w.hub.called("dispatch")) != 0 {
		t.Errorf("calls %v", w.hub.calls)
	}
}

func TestForceSkipsVerifyAndSaysSo(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v2, `echo "break x .: y"; exit 1`, "")})
	v, err := Engine(w.deps(t), Options{Force: true})
	if err != nil || v != "opened #1 for "+v2 {
		t.Fatalf("%q %v", v, err)
	}
	if c := w.hub.called("create-pull"); len(c) != 1 || !strings.Contains(c[0], "--force") {
		t.Errorf("%v", c)
	}
}

func TestASelftestFailureOpensNoPR(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v1, "exit 0", "")})
	if _, err := Engine(w.deps(t), Options{}); err == nil || !strings.Contains(err.Error(), "selftest") {
		t.Errorf("%v", err)
	}
	if len(w.hub.called("create-pull")) != 0 {
		t.Error("opened a PR")
	}
}

// A candidate whose selftest fails a probe over the member opens nothing,
// forced or not, and the verdict names the probe.
func TestAFailedProbeSkipsTheUpdateForcedOrNot(t *testing.T) {
	for _, force := range []bool{false, true} {
		w := newWorld(t, settings.YAML)
		bin := []byte("#!/bin/sh\ncase \"$1 $2 $3\" in\n" +
			"\"selftest --repo " + w.repo + "\") echo \"version " + v2 + "\"; echo \"ok binary: x\"; echo \"fail hooks: PreToolUse → cn hook x\"; exit 1 ;;\n" +
			"*) exit 9 ;;\nesac\n")
		w.publish(t, v2, relOpts{binary: bin})
		v, err := Engine(w.deps(t), Options{Force: force})
		if err != nil || v != "skipped: selftest failed (hooks)" {
			t.Fatalf("force %v: %q %v", force, v, err)
		}
		if !strings.Contains(w.out.String(), "fail hooks: PreToolUse") {
			t.Errorf("the report was not printed:\n%s", w.out)
		}
		if len(w.hub.called("create-pull")) != 0 {
			t.Error("opened a PR")
		}
	}
}

func TestUpToDateNamesTheSkip(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v1, relOpts{})
	w.publish(t, v2, relOpts{deprecated: "held: canary red"})
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "up to date" {
		t.Fatalf("%q %v", v, err)
	}
	if !strings.Contains(w.out.String(), v2+" skipped: held") {
		t.Errorf("skip reason missing:\n%s", w.out)
	}
}

// pinOf is ver's real manifest integrity when the registry serves it, else
// a well-formed stand-in.
func (w *world) pinOf(t *testing.T, ver string) string {
	t.Helper()
	p, err := w.reg.client().Packument(pkg)
	if err != nil || p.Versions[ver].Dist.Integrity == "" {
		return strings.Replace(pin1, "A", "C", 10)
	}
	got, err := Fetch(FetchInput{Registry: w.reg.client(), Package: pkg, Version: ver, Packument: p, Roots: rootsOf(testRoot, otherRoot),
		CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Platform: version.Platform(), Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	return got.Integrity
}

// openUpdatePR pushes a pin-only branch for ver and registers its PR.
func (w *world) openUpdatePR(t *testing.T, n int, ver, ciConclusion string) string {
	t.Helper()
	branch := "claudinite/engine-" + ver
	gitRun(t, w.repo, "checkout", "-q", "-b", branch)
	_ = os.WriteFile(filepath.Join(w.repo, settings.RelPath(w.f)), []byte(settingsFor(w.f, ver, w.pinOf(t, ver))), 0o644)
	gitRun(t, w.repo, "commit", "-q", "-am", "pin")
	gitRun(t, w.repo, "push", "-q", "origin", branch)
	sha := w.head(t)
	gitRun(t, w.repo, "checkout", "-q", "main")
	gitRun(t, w.repo, "branch", "-q", "-D", branch)
	w.hub.pulls = append(w.hub.pulls, githubapi.PR{Number: n, Title: "Claudinite engine " + ver, Author: "github-actions[bot]", Labels: []string{"claudinite-update"}, HeadRef: branch, HeadSHA: sha, BaseRef: "main", State: "open"})
	if w.hub.next <= n {
		w.hub.next = n + 1
	}
	if ciConclusion != "" {
		w.hub.runs[sha] = []githubapi.Run{{HeadSHA: sha, Event: "workflow_dispatch", Status: "completed", Conclusion: ciConclusion, CreatedAt: "2026-10-01T00:00:00Z"}}
	}
	return sha
}

func TestLandsAGreenUpdatePRFirst(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.publish(t, v3, relOpts{})
	sha := w.openUpdatePR(t, 4, v2, "success")
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "landed "+v2 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	if got := w.hub.called("merge"); len(got) != 1 || got[0] != "merge 4 "+sha+" Claudinite engine "+v2 {
		t.Errorf("merge %v", got)
	}
	if out := gitRun(t, w.bare, "branch", "--list", "claudinite/*"); out != "" {
		t.Errorf("branch not deleted: %s", out)
	}
	if got := w.hub.called("dispatch"); len(got) != 1 || got[0] != "dispatch claudinite-ci.yml main pr=" {
		t.Errorf("dispatch %v", got)
	}
	if len(w.hub.called("create-pull")) != 0 {
		t.Error("considered a candidate after landing")
	}
}

// Only the run the updater dispatched counts: a gated pull_request run on
// the same head is not a verdict.
func TestAnUpdatePRWithOnlyAGatedRunWaits(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha := w.openUpdatePR(t, 4, v2, "")
	w.hub.runs[sha] = []githubapi.Run{{HeadSHA: sha, Event: "pull_request", Status: "completed", Conclusion: "success", CreatedAt: "2026-10-01T00:00:00Z"}}
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "skipped: #4 for "+v2+" is open and has no CI run; dispatched its CI again" {
		t.Fatalf("%q %v", v, err)
	}
}

func TestAFailedUpdatePRStaysOpen(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.openUpdatePR(t, 4, v2, "failure")
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "skipped: #4 for "+v2+" is open and its CI concluded failure" {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	if len(w.hub.called("merge")) != 0 || len(w.hub.called("close-pull")) != 0 {
		t.Errorf("calls %v", w.hub.calls)
	}
}

func TestTwoUpdatePRsIsAnError(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v3, relOpts{})
	w.openUpdatePR(t, 4, v2, "failure")
	w.openUpdatePR(t, 5, v3, "failure")
	if _, err := Engine(w.deps(t), Options{}); err == nil || !strings.Contains(err.Error(), "#4") || !strings.Contains(err.Error(), "#5") {
		t.Errorf("%v", err)
	}
}

func TestANewerCandidateSupersedesTheOpenPR(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v3, relOpts{})
	w.openUpdatePR(t, 4, v2, "failure")
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "opened #5 for "+v3 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	if got := w.hub.called("close-pull"); len(got) != 1 || got[0] != "close-pull 4" {
		t.Errorf("close %v", got)
	}
	if got := w.hub.called("comment 4"); len(got) != 1 || !strings.Contains(got[0], "#5") {
		t.Errorf("comment %v", got)
	}
	if out := gitRun(t, w.bare, "branch", "--list", "claudinite/engine-"+v2); out != "" {
		t.Errorf("superseded branch kept: %s", out)
	}
}

func TestLand(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha := w.openUpdatePR(t, 4, v2, "")
	if _, err := Land(w.deps(t), 4, "0000000000000000000000000000000000000000"); err == nil || !strings.Contains(err.Error(), "moved") {
		t.Errorf("a moved head landed: %v", err)
	}
	v, err := Land(w.deps(t), 4, sha)
	if err != nil || v != "landed "+v2 {
		t.Fatalf("%q %v", v, err)
	}
	if got := w.hub.called("dispatch"); len(got) != 1 || got[0] != "dispatch claudinite-ci.yml main pr=" {
		t.Errorf("dispatch %v", got)
	}
}

func TestLandRefusesWhatIsNotAPinOnlyUpdatePR(t *testing.T) {
	cases := map[string]func(w *world, t *testing.T){
		"a person's PR": func(w *world, t *testing.T) { w.hub.pulls[0].Author = "someone" },
		"no label":      func(w *world, t *testing.T) { w.hub.pulls[0].Labels = nil },
		"another base":  func(w *world, t *testing.T) { w.hub.pulls[0].BaseRef = "dev" },
		"more than the pin": func(w *world, t *testing.T) {
			gitRun(t, w.repo, "fetch", "-q", "origin", w.hub.pulls[0].HeadRef)
			gitRun(t, w.repo, "checkout", "-q", "FETCH_HEAD")
			_ = os.WriteFile(filepath.Join(w.repo, "RULES.md"), []byte("x\n"), 0o644)
			gitRun(t, w.repo, "add", "RULES.md")
			gitRun(t, w.repo, "commit", "-q", "-m", "more")
			gitRun(t, w.repo, "push", "-q", "-f", "origin", "HEAD:refs/heads/"+w.hub.pulls[0].HeadRef)
			w.hub.pulls[0].HeadSHA = w.head(t)
			gitRun(t, w.repo, "checkout", "-q", "main")
		},
		"more than the pin's values": func(w *world, t *testing.T) {
			gitRun(t, w.repo, "fetch", "-q", "origin", w.hub.pulls[0].HeadRef)
			gitRun(t, w.repo, "checkout", "-q", "FETCH_HEAD")
			p := filepath.Join(w.repo, settings.RelPath(w.f))
			b, _ := os.ReadFile(p)
			_ = os.WriteFile(p, bytes.Replace(b, []byte("# kept"), []byte("# changed"), 1), 0o644)
			gitRun(t, w.repo, "commit", "-q", "-am", "more")
			gitRun(t, w.repo, "push", "-q", "-f", "origin", "HEAD:refs/heads/"+w.hub.pulls[0].HeadRef)
			w.hub.pulls[0].HeadSHA = w.head(t)
			gitRun(t, w.repo, "checkout", "-q", "main")
		},
	}
	for name, mutate := range cases {
		w := newWorld(t, settings.YAML)
		w.publish(t, v2, relOpts{})
		w.openUpdatePR(t, 4, v2, "")
		mutate(w, t)
		if _, err := Land(w.deps(t), 4, w.hub.pulls[0].HeadSHA); err == nil {
			t.Errorf("%s: landed", name)
		}
		if len(w.hub.called("merge")) != 0 {
			t.Errorf("%s: merged", name)
		}
	}
}

// The verdict is the last stdout line and takes one of these forms; T9's
// live steps and the workflow's summary read it.
func TestVerdictForms(t *testing.T) {
	want := []string{`^landed \S+$`, `^opened #\d+ for \S+$`, `^landed packs .+$`, `^opened #\d+ for packs .+$`, `^landed plan \S+$`, `^opened #\d+ for plan \S+$`, `^no PR: .+$`, `^skipped: .+$`, `^up to date$`}
	if strings.Join(VerdictForms, " ") != strings.Join(want, " ") {
		t.Errorf("forms %v", VerdictForms)
	}
	for _, v := range []string{"landed 1.2.0", "opened #3 for 1.2.0", "no PR: 1.2.0 would break this repo", "skipped: main is not green (failure)", "up to date",
		"landed packs hello 1.1, node 60928.2", "opened #4 for packs hello 1.1", "no PR: hello 1.2 fails this repo's checks", "skipped: engine PR #3 is open"} {
		if !IsVerdict(v) {
			t.Errorf("%q matches no form", v)
		}
	}
	if IsVerdict("landed") || IsVerdict("error: x") {
		t.Error("a non-verdict matched")
	}
}

const sampleDiff = "--- a/.github/workflows/claudinite-ci.yml\n+++ b/.github/workflows/claudinite-ci.yml\n@@ -1,1 +1,1 @@\n-old\n+new\n"

func TestAWorkflowChangeIsFiledAsAnIssue(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v2, "exit 0", sampleDiff)})
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #1 for "+v2 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	if got := w.hub.called("create-issue"); len(got) != 1 || got[0] != "create-issue Claudinite engine "+v2+" needs a workflow change|claudinite-update" {
		t.Fatalf("issues %v", got)
	}
	body := w.hub.issues[0].Body
	if !strings.Contains(body, "```diff\n"+sampleDiff+"```") || !strings.Contains(body, "stays on the current workflows until a person commits") {
		t.Errorf("body:\n%s", body)
	}
	if idx := strings.Index(strings.Join(w.hub.calls, "\n"), "create-issue"); idx < strings.Index(strings.Join(w.hub.calls, "\n"), "create-pull") {
		t.Error("the issue was filed before the PR")
	}
}

func TestAnOpenWorkflowIssueIsUpdatedNotDuplicated(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v2, "exit 0", sampleDiff)})
	w.hub.issues = []githubapi.Issue{{Number: 9, Title: "Claudinite engine " + v2 + " needs a workflow change", Body: "stale"}}
	w.hub.next = 10
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #10 for "+v2 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	if len(w.hub.called("create-issue")) != 0 || len(w.hub.called("update-issue 9")) != 1 || !strings.Contains(w.hub.issues[0].Body, sampleDiff) {
		t.Errorf("calls %v", w.hub.calls)
	}
}

func TestEqualWorkflowsFileNoIssue(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	if _, err := Engine(w.deps(t), Options{}); err != nil {
		t.Fatal(err)
	}
	if len(w.hub.called("create-issue")) != 0 || len(w.hub.called("issues")) != 0 {
		t.Errorf("calls %v", w.hub.calls)
	}
}

func TestARevokedPinFilesOneIssue(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v1, relOpts{deprecated: "revoked: leaks a token"})
	w.publish(t, v2, relOpts{})
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #1 for "+v2 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	if got := w.hub.called("create-issue"); len(got) != 1 || got[0] != "create-issue Claudinite engine "+v1+" is revoked|claudinite-update" {
		t.Fatalf("issues %v", got)
	}
	body := w.hub.issues[0].Body
	for _, s := range []string{"leaks a token", v2, "#1"} {
		if !strings.Contains(body, s) {
			t.Errorf("body lacks %q:\n%s", s, body)
		}
	}
	// The next night: the PR is open and still waiting; the issue is
	// updated, not duplicated.
	w.hub.runs[w.hub.pulls[0].HeadSHA] = nil
	if v, err := Engine(w.deps(t), Options{}); err != nil || !strings.HasPrefix(v, "skipped: #1") {
		t.Fatalf("%q %v", v, err)
	}
	if len(w.hub.called("create-issue")) != 1 {
		t.Errorf("duplicated: %v", w.hub.calls)
	}
}

func TestARevokedPinWithNothingNewerStillFilesTheIssue(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v1, relOpts{deprecated: "revoked: rehearsal"})
	w.publish(t, v2, relOpts{deprecated: "held: rehearsal"})
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "up to date" {
		t.Fatalf("%q %v", v, err)
	}
	if got := w.hub.called("create-issue"); len(got) != 1 || !strings.Contains(w.hub.issues[0].Body, "No allowed version") {
		t.Errorf("%v %+v", got, w.hub.issues)
	}
}

func TestAPinNotRevokedFilesNoRevocationIssue(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v1, relOpts{})
	w.publish(t, v2, relOpts{deprecated: "revoked: x"})
	if _, err := Engine(w.deps(t), Options{}); err != nil {
		t.Fatal(err)
	}
	if len(w.hub.called("create-issue")) != 0 {
		t.Errorf("%v", w.hub.calls)
	}
}

// Land checks trust, not only shape: the version must be newer than main's
// pin and pass the world guard's pin check.
func TestLandRefusesAnUntrustedPin(t *testing.T) {
	cases := map[string]func(w *world, t *testing.T) string{
		"held": func(w *world, t *testing.T) string {
			w.publish(t, v2, relOpts{deprecated: "held: canary red"})
			return "held"
		},
		"a lower version": func(w *world, t *testing.T) string {
			w.publish(t, "60930.0.5", relOpts{})
			return "not newer"
		},
		"a manifest the roots do not sign": func(w *world, t *testing.T) string {
			w.publish(t, v2, relOpts{issuer: otherRoot})
			return "signature"
		},
	}
	for name, setup := range cases {
		w := newWorld(t, settings.YAML)
		want := setup(w, t)
		ver := v2
		if name == "a lower version" {
			ver = "60930.0.5"
		}
		sha := w.openUpdatePR(t, 4, ver, "success")
		if _, err := Land(w.deps(t), 4, sha); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, want)
		}
		if len(w.hub.called("merge")) != 0 {
			t.Errorf("%s: merged", name)
		}
	}
}

func TestAnUpdatePRWhoseCIDidNotRunIsDispatchedAgain(t *testing.T) {
	for _, state := range []string{"", "cancelled", "timed_out"} {
		w := newWorld(t, settings.YAML)
		w.publish(t, v2, relOpts{})
		w.openUpdatePR(t, 4, v2, state)
		v, err := Engine(w.deps(t), Options{})
		if err != nil || !strings.HasPrefix(v, "skipped: #4 for "+v2+" is open and ") || !strings.HasSuffix(v, "; dispatched its CI again") {
			t.Errorf("%q: %q %v", state, v, err)
		}
		if got := w.hub.called("dispatch"); len(got) != 1 || got[0] != "dispatch claudinite-ci.yml claudinite/engine-"+v2+" pr=4" {
			t.Errorf("%q: dispatches %v", state, got)
		}
	}
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.openUpdatePR(t, 4, v2, "failure")
	if _, err := Engine(w.deps(t), Options{}); err != nil || len(w.hub.called("dispatch")) != 0 {
		t.Errorf("a failed run was dispatched again: %v %v", err, w.hub.calls)
	}
}

func TestAnUnlabelledUpdatePRIsRelabelledNotDuplicated(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.openUpdatePR(t, 4, v2, "failure")
	w.hub.pulls[0].Labels = nil
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "skipped: #4 for "+v2+" is open and its CI concluded failure" {
		t.Fatalf("%q %v", v, err)
	}
	if got := w.hub.called("label"); len(got) != 1 || got[0] != "label 4 claudinite-update" {
		t.Errorf("labels %v", got)
	}
	if len(w.hub.called("create-pull")) != 0 {
		t.Error("opened a second PR")
	}
	// A person's PR on such a branch is not ours.
	w = newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.openUpdatePR(t, 4, v2, "failure")
	w.hub.pulls[0].Labels, w.hub.pulls[0].Author = nil, "someone"
	if _, err := Engine(w.deps(t), Options{}); err != nil || len(w.hub.called("label 4")) != 0 {
		t.Errorf("relabelled a person's PR: %v %v", err, w.hub.calls)
	}
}

func TestAnUpdatePRForAVersionNowHeldIsClosed(t *testing.T) {
	for reason, kind := range map[string]string{"held: canary red": "held", "revoked: leak": "revoked", "old": "deprecated"} {
		w := newWorld(t, settings.YAML)
		w.publish(t, v2, relOpts{deprecated: reason})
		w.openUpdatePR(t, 4, v2, "failure")
		v, err := Engine(w.deps(t), Options{})
		if err != nil || v != "up to date" {
			t.Errorf("%s: %q %v", kind, v, err)
		}
		if got := w.hub.called("comment 4"); len(got) != 1 || !strings.Contains(got[0], kind) {
			t.Errorf("%s: comment %v", kind, got)
		}
		if got := w.hub.called("close-pull"); len(got) != 1 || got[0] != "close-pull 4" {
			t.Errorf("%s: close %v", kind, got)
		}
		if out := gitRun(t, w.bare, "branch", "--list", "claudinite/*"); out != "" {
			t.Errorf("%s: branch kept: %s", kind, out)
		}
	}
}

// The member file states the pin, so the engine update PR restates it
// beside the settings edit.
func TestProposeRestatesTheMemberFile(t *testing.T) {
	w := newWorld(t, settings.YAML)
	for rel, body := range map[string]string{
		settings.RelPath(settings.YAML):          settingsFor(settings.YAML, v1, pin1) + "packs:\n  declared:\n    - hello\n",
		".claudinite/shared/packs/hello/pack.json": `{"version": "1.0", "minEngineVersion": "0.0.0"}`,
	} {
		p := filepath.Join(w.repo, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, w.repo, "add", "-A")
	gitRun(t, w.repo, "commit", "-q", "-m", "declare hello")
	gitRun(t, w.repo, "push", "-q", "origin", "main")
	w.mainRun(t, "success")
	w.publish(t, v2, relOpts{})
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #1 for "+v2 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	branch := "claudinite/engine-" + v2
	if got := gitRun(t, w.bare, "diff", "--name-only", "main", branch); got != ".claudinite/flat/member.GENERATED.json\n"+settings.RelPath(settings.YAML) {
		t.Errorf("branch changes %q", got)
	}
	member := gitRun(t, w.bare, "show", branch+":.claudinite/flat/member.GENERATED.json")
	if !strings.Contains(member, `"version": "`+v2+`"`) || !strings.Contains(member, `"hello": "1.0"`) {
		t.Errorf("the member file does not state the new pin and the held pack:\n%s", member)
	}
}
