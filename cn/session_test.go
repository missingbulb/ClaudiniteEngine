package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

func writeJSON(t *testing.T, dir, name string, v any) string {
	t.Helper()
	raw, _ := json.Marshal(v)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const sessionTaskPath = ".claudinite/local/packs/acme-pack/tasks/a/task.md"

func heldItem() map[string]any {
	return map[string]any{"number": 7, "title": "[claudinite-work] acme-pack/a", "state": "open",
		"labels": []map[string]any{{"name": workitem.StatusRunningAgent}}, "body": sessionTaskPath + "\n"}
}

func TestConvergePrintsTheTransitionAndRefusesAnItemItDoesNotHold(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := writeJSON(t, dir, "item.json", heldItem())
	var out bytes.Buffer
	if err := cmdWork([]string{"converge", "--issue", "7", "--outcome", "done", "--summary", "ran it", "--repo", "o/r", "--item-file", f}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "the transition below is yours to execute") || !strings.Contains(out.String(), "`issue_write`") || !strings.Contains(out.String(), "state_reason `completed`") {
		t.Error(out.String())
	}
	closed := heldItem()
	closed["state"] = "closed"
	err := cmdWork([]string{"converge", "--issue", "7", "--outcome", "done", "--summary", "x", "--repo", "o/r", "--item-file", writeJSON(t, dir, "closed.json", closed)}, &out)
	if report.CodeOf(err) != report.Verify || !strings.Contains(err.Error(), "already closed") {
		t.Error(err)
	}
	err = cmdWork([]string{"converge", "--issue", "7", "--outcome", "great", "--summary", "x", "--repo", "o/r", "--item-file", f}, &out)
	if report.CodeOf(err) != report.Usage || !strings.Contains(err.Error(), "--outcome") {
		t.Error(err)
	}
	err = cmdWork([]string{"converge", "--issue", "7", "--outcome", "done", "--summary", "x", "--repo", "o/r", "--item-file", writeJSON(t, dir, "junk.json", "nope")}, &out)
	if report.CodeOf(err) != report.Usage || !strings.Contains(err.Error(), "issue_read") {
		t.Error(err)
	}
}

func TestRecordExecPrintsTheLineOrNamesTheBadArgument(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := cmdWork([]string{"record-exec", "acme-pack/a", "#7", "success"}, &out); err != nil || out.String() != "claudinite-task-exec v1 acme-pack/a [#7] success\n" {
		t.Error(out.String(), err)
	}
	if err := cmdWork([]string{"record-exec", "acme-pack/a", "#7"}, &out); report.CodeOf(err) != report.Usage {
		t.Error(err)
	}
}

func sessionRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for rel, body := range map[string]string{
		".claudinite/settings.yaml":                           "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - local/acme-pack\n",
		".claudinite/local/packs/acme-pack/pack.json":         `{}`,
		".claudinite/local/packs/acme-pack/tasks/a/task.md":   "do the thing\n",
		".claudinite/local/packs/acme-pack/tasks/a/task.json": `{"id": "a", "trigger": "request", "expected_outcome": "fresh_pr", "agent_model": "sonnet", "agent_instructions": "task.md", "agent_execution_timeout": 3600}`,
	} {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

// The engine's own update carries no task file in the member, so validate
// prints the instructions the engine carries for its agent stage.
func TestValidatePrintsTheEngineUpdatesInstructions(t *testing.T) {
	t.Parallel()
	repo, dir := sessionRepo(t), t.TempDir()
	held := map[string]any{"number": 8, "title": "[claudinite-work] engine/update", "state": "open",
		"labels": []map[string]any{{"name": workitem.StatusRunningAgent}}, "body": taskspec.UpdateTaskPath + "\n"}
	item := writeJSON(t, dir, "item.json", held)
	comments := writeJSON(t, dir, "comments.json", []world.Comment{{ID: 1, Body: execute.HandoffComment("E1", "8-n")}})
	var out bytes.Buffer
	if err := cmdWorkValidate([]string{"--repo", repo, "--issue", "8", "--nonce", "8-n", "--item-file", item, "--comments-file", comments}, &out); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"engine/update", "model: sonnet", workflows.StagingDir, "claudinite-ci.yml", "task:status:needs-human-decision"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("lacks %q:\n%s", s, out.String())
		}
	}
}

func TestValidateGatesTheSessionOnTheNonce(t *testing.T) {
	t.Parallel()
	repo, dir := sessionRepo(t), t.TempDir()
	item := writeJSON(t, dir, "item.json", heldItem())
	comments := writeJSON(t, dir, "comments.json", []world.Comment{
		{ID: 1, Body: execute.HandoffComment("E1", "7-n")},
	})
	var out bytes.Buffer
	args := []string{"--repo", repo, "--issue", "7", "--nonce", "7-n", "--item-file", item, "--comments-file", comments}
	if err := cmdWorkValidate(args, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "acme-pack/a") || !strings.Contains(out.String(), sessionTaskPath) {
		t.Error(out.String())
	}
	err := cmdWorkValidate([]string{"--repo", repo, "--issue", "7", "--nonce", "7-old", "--item-file", item, "--comments-file", comments}, &out)
	if report.CodeOf(err) != report.Verify || !strings.Contains(err.Error(), "not this item's session") {
		t.Error(err)
	}
	err = cmdWorkValidate([]string{"--repo", repo, "--issue", "7", "--item-file", item, "--comments-file", comments}, &out)
	if report.CodeOf(err) != report.Usage {
		t.Error("the nonce is required:", err)
	}
}

// A task whose outcome delivers a pull request is handed the repo's
// delivery and the procedure that lands it; one that opens none is not.
func TestValidatePrintsTheDeliveryOnlyForATaskThatDeliversAPR(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	item := writeJSON(t, dir, "item.json", heldItem())
	comments := writeJSON(t, dir, "comments.json", []world.Comment{{ID: 1, Body: execute.HandoffComment("E1", "7-n")}})
	validate := func(repo string) string {
		var out bytes.Buffer
		if err := cmdWorkValidate([]string{"--repo", repo, "--issue", "7", "--nonce", "7-n", "--item-file", item, "--comments-file", comments}, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	pr := validate(sessionRepo(t))
	if !strings.Contains(pr, "delivery: auto-merge\n") || !strings.Contains(pr, "# Delivering a scheduled task's pull request") {
		t.Errorf("a PR task:\n%s", pr)
	}
	repo := sessionRepo(t)
	decl := filepath.Join(repo, ".claudinite/local/packs/acme-pack/tasks/a/task.json")
	if err := os.WriteFile(decl, []byte(`{"id": "a", "trigger": "request", "expected_outcome": "no_code_changes", "agent_model": "sonnet", "agent_instructions": "task.md", "agent_execution_timeout": 3600}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if none := validate(repo); strings.Contains(none, "delivery:") || strings.Contains(none, "# Delivering") {
		t.Errorf("a task that opens no PR:\n%s", none)
	}
}

func TestWorkInstructionsPrintsTheRoutineProcedure(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := cmdWork([]string{"instructions"}, &out); err != nil || !strings.HasPrefix(out.String(), "# Executing one Claudinite work item") {
		t.Errorf("%v:\n%.200s", err, out.String())
	}
	if err := cmdWork([]string{"instructions", "extra"}, &out); report.CodeOf(err) != report.Usage {
		t.Error(err)
	}
}
