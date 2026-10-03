package execute

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
)

const rosterPath = ".claudinite/fleet/roster.GENERATED.json"

// checkout is a clone of a bare origin whose main holds one commit.
func checkout(t *testing.T) (root, origin string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	origin, root = filepath.Join(dir, "origin.git"), filepath.Join(dir, "root")
	sh := func(d string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	sh(dir, "init", "--quiet", "--bare", "--initial-branch=main", origin)
	sh(dir, "clone", "--quiet", origin, root)
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("r\n"), 0o644)
	sh(root, "add", "README.md")
	sh(root, "commit", "--quiet", "-m", "first")
	sh(root, "push", "--quiet", "origin", "HEAD:main")
	return root, origin
}

func remoteFile(t *testing.T, origin, ref, path string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", origin, "show", ref+":"+path).Output()
	if err != nil {
		t.Fatalf("%s has no %s: %v", ref, path, err)
	}
	return string(out)
}

func remoteMessage(t *testing.T, origin, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", origin, "log", "-1", "--format=%B", ref).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func deliveringWorker(t *testing.T, root string, gh *sdkWorld) CodeWorker {
	w := worker(t)
	w.Place.Root = root
	git := gitcmd.Repo{Dir: root}
	w.SDK = func(task taskspec.Task, _ workitem.Issue) *SDK {
		return &SDK{Pack: task.Pack, Task: task.ID, Granted: []string{"openPr"}, Git: git.Run, GitHub: gh, DefaultBranch: "main", Log: func(string) {}}
	}
	return w
}

func clean(t *testing.T, root string) {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil || len(out) != 0 {
		t.Errorf("the checkout was left changed: %q %v", out, err)
	}
}

const writeRoster = `mkdir -p "$CLAUDINITE_REPO_ROOT/.claudinite/fleet" && printf '%s\n' "$ROSTER" > "$CLAUDINITE_REPO_ROOT/` + rosterPath + `"`

// A shell code_work's tree change under an outcome that opens a pull
// request is committed with the task's trailers, pushed to the target
// branch and opened there; amended, it reaches the same pull request; and
// the checkout is put back each time.
func TestAShellCodeWorksTreeChangeIsDelivered(t *testing.T) {
	root, origin := checkout(t)
	gh := newSDKWorld(t)
	w := deliveringWorker(t, root, gh)
	tk := shellTask(t, writeRoster, map[string]any{"expected_outcome": "amend_existing_or_create_new_pr", "automerge": []any{"fleet-roster-artifact"}})
	branch := "claudinite/acme-pack/a/2026-10-03-x"

	w.Env["ROSTER"] = "one"
	res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeFresh, Branch: branch}})
	if !res.OK || res.DeliveredPR == 0 || res.Branch != branch {
		t.Fatalf("%+v", res)
	}
	if got := remoteFile(t, origin, branch, rosterPath); got != "one\n" {
		t.Errorf("pushed %q", got)
	}
	msg := remoteMessage(t, origin, branch)
	for _, want := range []string{"acme-pack/a: code-work for #4", rosterPath, "Claudinite-Task: acme-pack/a", "Claudinite-Automerge-Policy: fleet-roster-artifact"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the commit lacks %q:\n%s", want, msg)
		}
	}
	clean(t, root)

	w.Env["ROSTER"] = "two"
	again := w.Run(tk, Work{Item: workitem.Issue{Number: 5}, Target: Target{Mode: ModeAmend, Branch: branch, PR: res.DeliveredPR}})
	if !again.OK || again.DeliveredPR != res.DeliveredPR {
		t.Fatalf("the amend reached %+v, not #%d", again, res.DeliveredPR)
	}
	if got := remoteFile(t, origin, branch, rosterPath); got != "two\n" {
		t.Errorf("amended %q", got)
	}
	if pulls, _ := gh.repo.PullsPage("open", "", "", 1); len(pulls) != 1 {
		t.Errorf("%d pull requests, want the one amended", len(pulls))
	}
	clean(t, root)

	// The same change again is the tree the branch holds: the pull request
	// is answered and nothing is pushed.
	headOf := func() string {
		out, err := exec.Command("git", "--git-dir", origin, "rev-parse", branch).Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	before := headOf()
	same := w.Run(tk, Work{Item: workitem.Issue{Number: 6}, Target: Target{Mode: ModeAmend, Branch: branch, PR: res.DeliveredPR}})
	if !same.OK || same.DeliveredPR != res.DeliveredPR || headOf() != before {
		t.Errorf("an unchanged recompute pushed or lost the pull request: %+v", same)
	}
	clean(t, root)

	// A change the base branch already landed, behind the checkout's own
	// HEAD, delivers nothing.
	other := filepath.Join(t.TempDir(), "other")
	for _, args := range [][]string{{"clone", "--quiet", origin, other}, {"-C", other, "checkout", "--quiet", "-B", "main", "origin/main"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	_ = os.MkdirAll(filepath.Join(other, ".claudinite/fleet"), 0o755)
	_ = os.WriteFile(filepath.Join(other, rosterPath), []byte("merged\n"), 0o644)
	for _, args := range [][]string{{"-C", other, "add", "-A"}, {"-C", other, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--quiet", "-m", "merged"}, {"-C", other, "push", "--quiet", "origin", "HEAD:main"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	w.Env["ROSTER"] = "merged"
	if res := w.Run(tk, Work{Item: workitem.Issue{Number: 7}, Target: Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/again"}}); !res.OK || res.DeliveredPR != 0 {
		t.Errorf("a change the base already holds delivered %+v", res)
	}
	clean(t, root)

	// A run that leaves nothing new delivers nothing.
	w.Env["ROSTER"] = "two"
	tk = shellTask(t, "true", map[string]any{"expected_outcome": "amend_existing_or_create_new_pr"})
	if res := w.Run(tk, Work{Item: workitem.Issue{Number: 6}, Target: Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/other"}}); !res.OK || res.DeliveredPR != 0 {
		t.Errorf("a run with no change delivered %+v", res)
	}
}

// An outcome that opens no pull request delivers nothing, and the
// change stays where the code-work left it.
func TestAShellCodeWorkWithNoTargetDeliversNothing(t *testing.T) {
	root, _ := checkout(t)
	gh := newSDKWorld(t)
	w := deliveringWorker(t, root, gh)
	w.Env["ROSTER"] = "one"
	res := w.Run(shellTask(t, writeRoster, nil), Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeNone}})
	if !res.OK || res.DeliveredPR != 0 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, rosterPath)); err != nil {
		t.Error("the change was removed")
	}
}

// A module worker that delivers the same file through the SDK and a shell
// code_work whose change the executor delivers reach the same pull
// request.
func TestAModuleWorkerAndAShellCodeWorkReachTheSamePR(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	root, origin := checkout(t)
	dir, err := runner.Unpack(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gh := newSDKWorld(t)
	w := deliveringWorker(t, root, gh)
	w.Runner = runner.Runner{Dir: dir, Engine: "0.0.0-test"}
	branch := "claudinite/acme-pack/a/2026-10-03-y"
	mod := loopTask("a", map[string]any{"agent_model": "none", "code_worker_mjs": "worker.mjs", "code_work_timeout": 30,
		"expected_outcome": "amend_existing_or_create_new_pr", "automerge": []any{"fleet-roster-artifact"}})
	mod.Dir = t.TempDir()
	_ = os.WriteFile(filepath.Join(mod.Dir, "worker.mjs"), []byte(`import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { git, github, commitMessage } from '@claudinite/sdk';
const must = async (...args) => { const r = await git(...args); if (r.code !== 0) throw new Error(args[0] + ': ' + r.stderr); return r.stdout; };
export async function worker(params) {
  const root = process.env.CLAUDINITE_REPO_ROOT;
  mkdirSync(join(root, '.claudinite/fleet'), { recursive: true });
  writeFileSync(join(root, '`+rosterPath+`'), 'one\n');
  await must('add', '`+rosterPath+`');
  await must('commit', '--quiet', '-m', commitMessage('Roster'));
  await must('push', '--quiet', '--force', 'origin', 'HEAD:refs/heads/' + params.target.branch);
  await must('reset', '--quiet', '--hard', 'HEAD~1');
  await github.openPr({ title: 'Roster', body: 'b', head: params.target.branch });
}
`), 0o644)
	first := w.Run(mod, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeFresh, Branch: branch}})
	if !first.OK || first.DeliveredPR == 0 {
		t.Fatalf("the module worker: %+v", first)
	}
	if !strings.Contains(remoteMessage(t, origin, branch), "Claudinite-Task: acme-pack/a") {
		t.Error("the module worker's commit lacks the task trailer")
	}
	clean(t, root)

	w.Env["ROSTER"] = "two"
	sh := shellTask(t, writeRoster, map[string]any{"expected_outcome": "amend_existing_or_create_new_pr", "automerge": []any{"fleet-roster-artifact"}})
	second := w.Run(sh, Work{Item: workitem.Issue{Number: 5}, Target: Target{Mode: ModeAmend, Branch: branch, PR: first.DeliveredPR}})
	if !second.OK || second.DeliveredPR != first.DeliveredPR || second.Branch != first.Branch {
		t.Fatalf("the shell code_work reached %+v, the module worker %+v", second, first)
	}
	if got := remoteFile(t, origin, branch, rosterPath); got != "two\n" {
		t.Errorf("the branch holds %q", got)
	}
	clean(t, root)
}

// A shell code_work that fails leaves the shared checkout as it found it,
// though it wrote its change before failing.
func TestAFailedShellCodeWorkPutsTheCheckoutBack(t *testing.T) {
	root, _ := checkout(t)
	w := deliveringWorker(t, root, newSDKWorld(t))
	w.Env["ROSTER"] = "one"
	tk := shellTask(t, writeRoster+" && exit 1", map[string]any{"expected_outcome": "amend_existing_or_create_new_pr"})
	if res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/x"}}); res.OK {
		t.Fatalf("%+v", res)
	}
	clean(t, root)
}

// The executor delivers only onto a branch under its own root, so an
// amend target from a collaborator's pull request is never force-pushed.
func TestAShellDeliveryRefusesABranchOutsideTheExecutorsRoot(t *testing.T) {
	root, origin := checkout(t)
	w := deliveringWorker(t, root, newSDKWorld(t))
	w.Env["ROSTER"] = "one"
	tk := shellTask(t, writeRoster, map[string]any{"expected_outcome": "amend_existing_or_create_new_pr"})
	for _, branch := range []string{"feature/theirs", "claudinite", "refs/heads/main"} {
		res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeAmend, Branch: branch, PR: 9}})
		if res.OK || res.DeliveredPR != 0 {
			t.Errorf("%s: delivered %+v", branch, res)
		}
		if out, _ := exec.Command("git", "--git-dir", origin, "branch", "--list").Output(); strings.TrimSpace(string(out)) != "* main" && strings.TrimSpace(string(out)) != "main" {
			t.Errorf("%s: origin holds %q", branch, out)
		}
		clean(t, root)
	}
}

// Under a declared policy the commit carries only the paths its allow
// terms cover; the rest are named and left out.
func TestAShellDeliveryCommitsWhatThePolicyCovers(t *testing.T) {
	root, origin := checkout(t)
	var logged []string
	w := deliveringWorker(t, root, newSDKWorld(t))
	w.Log = func(s string) { logged = append(logged, s) }
	w.Rules = mergepolicy.Compile([]mergepolicy.PackRules{{ID: "acme-pack", File: "merge-rules.json", Specs: []any{map[string]any{
		"name": "acme-roster", "pathMatching": `/^\.claudinite\/fleet\/roster\.GENERATED\.json$/`, "changeKinds": []any{"added", "modified"}, "editShape": "any"}}}})
	w.Env["ROSTER"] = "one"
	branch := "claudinite/acme-pack/a/y"
	tk := shellTask(t, writeRoster+` && echo stray > "$CLAUDINITE_REPO_ROOT/STRAY.md"`, map[string]any{"expected_outcome": "amend_existing_or_create_new_pr", "automerge": []any{"acme-roster"}})
	res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeFresh, Branch: branch}})
	if !res.OK || res.DeliveredPR == 0 {
		t.Fatalf("%+v", res)
	}
	out, _ := exec.Command("git", "--git-dir", origin, "diff", "--name-only", "main", branch).Output()
	if string(out) != rosterPath+"\n" {
		t.Errorf("the branch changes %q", out)
	}
	if !strings.Contains(strings.Join(logged, "\n"), "STRAY.md") {
		t.Errorf("the left-out path is not named: %v", logged)
	}
	clean(t, root)
}

// A symlink in the change is refused, never followed out of the checkout.
func TestAShellDeliveryRefusesASymlink(t *testing.T) {
	root, origin := checkout(t)
	secret := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secret, []byte("s3cret\n"), 0o644)
	w := deliveringWorker(t, root, newSDKWorld(t))
	w.Env["SECRET"] = secret
	tk := shellTask(t, `mkdir -p "$CLAUDINITE_REPO_ROOT/.claudinite/fleet" && ln -s "$SECRET" "$CLAUDINITE_REPO_ROOT/`+rosterPath+`"`, map[string]any{"expected_outcome": "amend_existing_or_create_new_pr"})
	res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/z"}})
	if res.OK || res.DeliveredPR != 0 || !strings.Contains(res.Detail, "symlink") {
		t.Errorf("a symlink was delivered: %+v", res)
	}
	if out, _ := exec.Command("git", "--git-dir", origin, "branch", "--list", "claudinite/*").Output(); len(out) != 0 {
		t.Errorf("pushed %q", out)
	}
	clean(t, root)
}
