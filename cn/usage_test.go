package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
)

// A code-work command runs in its task's folder, inside the checkout, so
// the checkout's top level is the root wherever nothing names one.
func TestCodeWorkRootFindsTheCheckoutFromTheTaskDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	task := filepath.Join(root, "packs", "acme-pack", "tasks", "acme-task")
	if err := os.MkdirAll(task, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(task)
	t.Setenv("CLAUDINITE_REPO_ROOT", "")
	if got, err := codeWorkRoot(""); err != nil || got != root {
		t.Errorf("from the task's folder: %q %v, want %q", got, err, root)
	}
	t.Setenv("CLAUDINITE_REPO_ROOT", "/named")
	if got, _ := codeWorkRoot(""); got != "/named" {
		t.Errorf("the executor's variable: %q", got)
	}
	if got, _ := codeWorkRoot("/given"); got != "/given" {
		t.Errorf("--repo: %q", got)
	}
}

// Every fold commit names the engine's task and the automerge it declares,
// which is what the landing lane reads the policy from.
func TestTheUsageFoldsCommitsCarryTheTaskAndItsAutomerge(t *testing.T) {
	tasks, errs := taskspec.Discover(t.TempDir(), nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	want := "Claudinite-Task: engine/usage-fold\nClaudinite-Automerge-Policy: rolling-usage-files;rolling-usage-file-moves"
	if got := usageFoldTrailers(tasks); got != want {
		t.Errorf("%q, want %q", got, want)
	}
	if got := usageFoldTrailers(nil); got != "Claudinite-Task: engine/usage-fold" {
		t.Errorf("no declaration to read: %q", got)
	}
}

func TestUsageTakesOnlyFold(t *testing.T) {
	for _, args := range [][]string{nil, {"census"}} {
		if err := cmdUsage(args, io.Discard); err == nil || !strings.Contains(err.Error(), "usage takes fold") {
			t.Errorf("%v: %v", args, err)
		}
	}
}
