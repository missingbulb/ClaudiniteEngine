package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/tasks/land"
)

func TestOnlyAPolicyThatAuthorizesALandingEntersTheLane(t *testing.T) {
	for _, c := range []struct {
		policy any
		want   bool
	}{{nil, false}, {"nothing", false}, {"anything", true}, {[]any{"layout"}, true}, {"reject:layout", false}} {
		if got := mayLand(c.policy); got != c.want {
			t.Errorf("%v: %v", c.policy, got)
		}
	}
}

// The landing lane's diff is the pull request's own: its head against
// its merge base with the default branch, additions, edits and deletions
// with the content on each side.
func TestPullDiffReadsThePullRequestsOwnDiff(t *testing.T) {
	root := t.TempDir()
	origin, work := filepath.Join(root, "origin.git"), filepath.Join(root, "work")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(root, "init", "-q", "--bare", "-b", "main", origin)
	git(root, "clone", "-q", origin, work)
	write := func(p, body string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(work, p)), 0o755)
		if err := os.WriteFile(filepath.Join(work, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/a.md", "old\n")
	write("gone.txt", "bye\n")
	git(work, "add", "-A")
	git(work, "commit", "-q", "-m", "base")
	git(work, "push", "-q", "origin", "HEAD:main")
	git(work, "checkout", "-q", "-b", "claudinite/acme-pack/acme-task/x")
	write("docs/a.md", "new\n")
	write("src/b.go", "package b\n")
	git(work, "rm", "-q", "gone.txt")
	git(work, "add", "-A")
	git(work, "commit", "-q", "-m", "work")
	git(work, "push", "-q", "origin", "HEAD:claudinite/acme-pack/acme-task/x")
	sha := git(work, "rev-parse", "HEAD")
	git(work, "checkout", "-q", "main")

	entries, err := pullDiff(gitcmd.Repo{Dir: work}, "main")(land.PR{Number: 7, HeadRef: "claudinite/acme-pack/acme-task/x", HeadSHA: sha})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.File] = e.ChangeKind()
	}
	want := map[string]string{"docs/a.md": "modified", "src/b.go": "added", "gone.txt": "deleted"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	for _, e := range entries {
		if e.File == "docs/a.md" && (*e.Before != "old\n" || *e.After != "new\n") {
			t.Errorf("docs/a.md: %q → %q", *e.Before, *e.After)
		}
	}
}

// The job token leaves the process environment the moment the executor's
// client takes it, and the work steps are handed it back as GITHUB_TOKEN,
// the surface execute.TaskEnv documents them writing through; nothing else
// read from the environment gains it.
func TestWorkStepsAreHandedTheJobToken(t *testing.T) {
	job := map[string]string{"PATH": "/bin"}
	got := workStepEnv(job, "ghs_job")
	if got["GITHUB_TOKEN"] != "ghs_job" || got["PATH"] != "/bin" {
		t.Errorf("%v", got)
	}
	if _, ok := job["GITHUB_TOKEN"]; ok {
		t.Error("the job's own map gained the token")
	}
	if _, ok := workStepEnv(job, "")["GITHUB_TOKEN"]; ok {
		t.Error("an absent token was handed on as an empty one")
	}
}
