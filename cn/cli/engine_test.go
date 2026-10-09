package main

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
)

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
