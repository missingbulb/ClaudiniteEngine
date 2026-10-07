package update

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// The scheduler the stand-in candidate stages for o/r.
const stagedContent = "staged scheduler for o/r\n"

// proposeStaged opens the update PR for v2 with a candidate whose
// workflows differ from the member's.
func proposeStaged(t *testing.T) (*world, EngineResult) {
	t.Helper()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v2, "exit 0", "staged scheduler")})
	got, err := EngineRun(w.deps(t), Options{})
	if err != nil || got.Verdict != "opened #1 for "+v2 {
		t.Fatalf("%+v %v\n%s", got, err, w.out)
	}
	w.hub.pulls[0].HeadSHA = gitRun(t, w.bare, "rev-parse", got.Branch)
	w.hub.pulls[0].Labels = []string{Label}
	return w, got
}

// A workflow the new engine needs changed rides the update PR, staged
// where the job token may push it, with no issue filed; the PR waits for
// the agent stage rather than have its CI dispatched.
func TestAWorkflowChangeIsStagedOnTheUpdatePR(t *testing.T) {
	t.Parallel()
	w, got := proposeStaged(t)
	branch := "claudinite/engine-" + v2
	if got.PR != 1 || got.Branch != branch || !reflect.DeepEqual(got.Staged, []string{stagedScheduler}) {
		t.Errorf("outcome %+v", got)
	}
	if files := gitRun(t, w.bare, "diff", "--name-only", "main", branch); files != stagedScheduler+"\n"+settings.RelPath(settings.YAML) {
		t.Errorf("branch changes %q", files)
	}
	if staged := gitRun(t, w.bare, "show", branch+":"+stagedScheduler) + "\n"; staged != stagedContent {
		t.Errorf("staged %q, want the candidate's content for this repo's name", staged)
	}
	if len(w.hub.called("create-issue")) != 0 || len(w.hub.called("issues")) != 1 {
		t.Errorf("issue calls %v", w.hub.calls)
	}
	if got := w.hub.called("dispatch"); len(got) != 0 {
		t.Errorf("dispatched CI before the workflows moved: %v", got)
	}
	body := w.hub.called("create-pull")[0]
	for _, s := range []string{stagedScheduler, ".github/workflows/claudinite-scheduler.yml", "agent stage", "pr="} {
		if !strings.Contains(body, s) {
			t.Errorf("PR body lacks %q:\n%s", s, body)
		}
	}
}

// Workflows already as the new engine expects stage nothing: the PR is the
// pin alone and its CI is dispatched as before.
func TestEqualWorkflowsStageNothing(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	got, err := EngineRun(w.deps(t), Options{})
	if err != nil || got.Verdict != "opened #1 for "+v2 || len(got.Staged) != 0 {
		t.Fatalf("%+v %v\n%s", got, err, w.out)
	}
	if files := gitRun(t, w.bare, "diff", "--name-only", "main", "claudinite/engine-"+v2); files != settings.RelPath(settings.YAML) {
		t.Errorf("branch changes %q", files)
	}
	if d := w.hub.called("dispatch"); len(d) != 1 {
		t.Errorf("dispatches %v", d)
	}
}

// The issue an earlier engine filed instead of a PR is closed by the run
// that delivers the PR, and the PR names it.
func TestAnOldWorkflowChangeIssueIsClosedByTheUpdatePR(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v2, "exit 0", "staged scheduler")})
	w.hub.issues = []githubapi.Issue{
		{Number: 9, Title: "Claudinite engine 1.61005.8 needs a workflow change", Body: "a patch"},
		{Number: 10, Title: "Claudinite engine " + v1 + " is revoked", Body: "x"},
	}
	w.hub.next = 11
	got, err := EngineRun(w.deps(t), Options{})
	if err != nil || got.PR != 11 {
		t.Fatalf("%+v %v\n%s", got, err, w.out)
	}
	if c := w.hub.called("close-issue"); !reflect.DeepEqual(c, []string{"close-issue 9"}) {
		t.Errorf("closed %v", c)
	}
	if c := w.hub.called("comment 9"); len(c) != 1 || !strings.Contains(c[0], "#11") {
		t.Errorf("comment %v", c)
	}
	if body := w.hub.called("create-pull")[0]; !strings.Contains(body, "#9") {
		t.Errorf("the PR does not name #9:\n%s", body)
	}
}

// An open update PR for the candidate whose staged workflows were never
// moved goes back to the agent stage, its CI not dispatched again.
func TestAnUpdatePRStillCarryingStagedWorkflowsIsHandedOffAgain(t *testing.T) {
	t.Parallel()
	w, first := proposeStaged(t)
	w.out.Reset()
	got, err := EngineRun(w.deps(t), Options{})
	if err != nil || got.PR != 1 || got.Branch != first.Branch || !reflect.DeepEqual(got.Staged, first.Staged) ||
		!strings.HasPrefix(got.Verdict, "skipped: #1 for "+v2+" is open and ") {
		t.Fatalf("%+v %v\n%s", got, err, w.out)
	}
	if d := w.hub.called("dispatch"); len(d) != 0 {
		t.Errorf("dispatched %v", d)
	}
	if len(w.hub.called("create-pull")) != 1 {
		t.Error("opened a second PR")
	}
}

// moveStaged does on the PR's branch what the agent stage does: each
// staged file moved unedited into .github/workflows/; edit then changes
// the branch further.
func moveStaged(t *testing.T, w *world, edit func(dir string)) string {
	t.Helper()
	branch := w.hub.pulls[0].HeadRef
	gitRun(t, w.repo, "fetch", "-q", "origin", branch)
	gitRun(t, w.repo, "checkout", "-q", "FETCH_HEAD")
	_ = os.MkdirAll(filepath.Join(w.repo, ".github", "workflows"), 0o755)
	gitRun(t, w.repo, "mv", stagedScheduler, ".github/workflows/claudinite-scheduler.yml")
	if edit != nil {
		edit(w.repo)
	}
	gitRun(t, w.repo, "add", "-A")
	gitRun(t, w.repo, "commit", "-q", "-m", "move the staged workflows")
	gitRun(t, w.repo, "push", "-q", "-f", "origin", "HEAD:refs/heads/"+branch)
	sha := w.head(t)
	w.hub.pulls[0].HeadSHA = sha
	gitRun(t, w.repo, "checkout", "-q", "main")
	return sha
}

// An engine PR carrying, beside the pin, exactly the workflows the new
// engine expects passes the landing gate, which CheckLand runs from the
// PR's base and head with no GitHub call; Land, whose job token GitHub
// refuses a workflow-changing merge, skips it without failing and leaves
// the merge to the agent stage.
func TestTheGateAcceptsExactlyTheExpectedWorkflowsAndLandLeavesTheMerge(t *testing.T) {
	t.Parallel()
	w, _ := proposeStaged(t)
	sha := moveStaged(t, w, nil)
	before := len(w.hub.calls)
	v, err := gateAt(t, w.repo, w.deps(t), w.hub.pulls[0])
	if err != nil || v != "ok: "+sha+" may land "+v2 {
		t.Fatalf("check: %q %v\n%s", v, err, w.out)
	}
	if got := writes(w.hub.calls[before:]); len(got) != 0 {
		t.Errorf("the gate wrote: %v", got)
	}
	noAPI := w.deps(t)
	noAPI.GitHub = nil
	if _, err := CheckLand(noAPI, "0123456789012345678901234567890123456789", sha); err == nil || !strings.Contains(err.Error(), "fetch") {
		t.Errorf("a base the checkout lacks: %v", err)
	}
	v, err = Land(w.deps(t), 1, sha)
	if err != nil || !strings.HasPrefix(v, "skipped: #1 changes .github/workflows/claudinite-scheduler.yml") || !strings.Contains(v, "agent stage") {
		t.Fatalf("land: %q %v\n%s", v, err, w.out)
	}
	if got := writes(w.hub.calls[before:]); len(got) != 0 {
		t.Errorf("land wrote on a workflow-changing PR: %v", got)
	}
	if out := gitRun(t, w.bare, "branch", "--list", w.hub.pulls[0].HeadRef); out == "" {
		t.Error("land deleted the branch its agent stage merges")
	}
}

// Anything under .github/workflows/ but exactly what the new engine
// expects, or staged files never moved, keeps the PR from landing.
func TestLandRefusesAnyOtherWorkflowEdit(t *testing.T) {
	t.Parallel()
	write := func(rel, body string) func(string) {
		return func(dir string) {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, []byte(body), 0o644)
		}
	}
	cases := map[string]struct {
		edit func(string)
		move bool
		want string
	}{
		"an edited move":                   {write(".github/workflows/claudinite-scheduler.yml", stagedContent+"      - run: curl evil\n"), true, "claudinite-scheduler.yml"},
		"a template the engine left alone": {write(".github/workflows/claudinite-ci.yml", "name: ci\n"), true, "claudinite-ci.yml"},
		"a workflow of the repo's own":     {write(".github/workflows/deploy.yml", "name: deploy\n"), true, "deploy.yml"},
		"staged files never moved":         {nil, false, "staged"},
	}
	for name, c := range cases {
		w, _ := proposeStaged(t)
		sha := w.hub.pulls[0].HeadSHA
		if c.move {
			sha = moveStaged(t, w, c.edit)
		}
		if _, err := Land(w.deps(t), 1, sha); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, c.want)
		}
		if _, err := gateAt(t, w.repo, w.deps(t), w.hub.pulls[0]); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the gate: %v, want an error naming %q", name, err, c.want)
		}
		if len(w.hub.called("merge")) != 0 {
			t.Errorf("%s: merged", name)
		}
	}

	// A deleted workflow is not a move.
	w := newWorld(t, settings.YAML)
	_ = os.MkdirAll(filepath.Join(w.repo, ".github", "workflows"), 0o755)
	_ = os.WriteFile(filepath.Join(w.repo, ".github", "workflows", "claudinite-ci.yml"), []byte("name: ci\n"), 0o644)
	gitRun(t, w.repo, "add", "-A")
	gitRun(t, w.repo, "commit", "-q", "-m", "ci")
	gitRun(t, w.repo, "push", "-q", "origin", "main")
	w.mainRun(t, "success")
	w.publish(t, v2, relOpts{binary: cnScript(v2, "exit 0", "staged scheduler")})
	if _, err := EngineRun(w.deps(t), Options{}); err != nil {
		t.Fatal(err)
	}
	w.hub.pulls[0].Labels = []string{Label}
	sha := moveStaged(t, w, func(dir string) { _ = os.Remove(filepath.Join(dir, ".github", "workflows", "claudinite-ci.yml")) })
	if _, err := Land(w.deps(t), 1, sha); err == nil || !strings.Contains(err.Error(), "claudinite-ci.yml") {
		t.Errorf("a deletion: %v", err)
	}

	// Without the repo's name the expected workflows cannot be computed.
	w, _ = proposeStaged(t)
	sha = moveStaged(t, w, nil)
	d := w.deps(t)
	d.FullName = ""
	if _, err := Land(d, 1, sha); err == nil || len(w.hub.called("merge")) != 0 {
		t.Errorf("landed workflows without the repo's name: %v", err)
	}
}

// gateAt fetches update PR pr's base (main) and head into repo, as the
// agent stage does before the gate, and returns the gate's verdict over
// them with no GitHub at all: a call would panic on the nil API.
func gateAt(t *testing.T, repo string, d Deps, pr githubapi.PR) (string, error) {
	t.Helper()
	gitRun(t, repo, "fetch", "-q", "origin", "+refs/heads/main:refs/gate/base", "+refs/heads/"+pr.HeadRef+":refs/gate/head")
	base := gitRun(t, repo, "rev-parse", "refs/gate/base")
	head := gitRun(t, repo, "rev-parse", "refs/gate/head")
	d.GitHub = nil
	return CheckLand(d, base, head)
}
