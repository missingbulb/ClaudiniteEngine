package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/tasks/land"
)

func keyed(state string, features ...string) *license.ActionsOnce {
	k := &license.KeyPayload{Typ: "actions", Plan: license.PlanPublic, State: state, Features: features}
	return &license.ActionsOnce{Request: func() license.ActionsResult { return license.ActionsResult{Key: k, Wire: "W"} }}
}

func TestOnlyAnItemNeedingTheKeyParksWithoutOne(t *testing.T) {
	member, agentic, growth := sessionTaskOf("acme-pack", "none"), sessionTaskOf("acme-pack", "sonnet"), sessionTaskOf("acme-engine", "none")
	growth.Engine = true
	asked := 0
	none := &license.ActionsOnce{Request: func() license.ActionsResult {
		asked++
		return license.ActionsResult{Cause: license.CauseNoOIDC, Detail: "id-token: write is missing"}
	}}
	if n := taskLicense(none, member); n != "" || asked != 0 {
		t.Error("a member's agentless task runs keyless, and asks for nothing:", n, asked)
	}
	if n := taskLicense(none, agentic); n == "" {
		t.Error("an agentic task without a key parks")
	}
	if n := taskLicense(keyed("degraded", "claudinite-tasks"), agentic); n == "" {
		t.Error("a degraded key parks the agentic item")
	}
	if n := taskLicense(keyed("ok"), growth); !strings.Contains(n, "claudinite-tasks") {
		t.Error("an engine pack's task needs the claudinite-tasks row:", n)
	}
	if n := taskLicense(keyed("ok", "claudinite-tasks"), growth); n != "" {
		t.Error(n)
	}
	if n := taskLicense(keyed("ok"), agentic); n != "" {
		t.Error("a member's agentic task needs a sound key, not the engine's row:", n)
	}
}

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

func sessionTaskOf(pack, model string) taskspec.Task {
	return taskspec.Task{Pack: pack, ID: "a", Decl: taskspec.Decl{"id": "a", "agent_model": model}}
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
