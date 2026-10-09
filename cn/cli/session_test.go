package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/workflows"
)

// The engine's own update carries no task file in the member, so validate
// prints the instructions the engine carries for its agent stage, which name
// the update's staging directory.
func TestValidatePrintsTheEngineUpdatesInstructions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	repo := t.TempDir()
	writeRepoFile(t, repo, ".claudinite/settings.yaml", "engine:\n  version: \"1.1.0\"\npacks:\n  declared: []\n")
	held := map[string]any{"number": 8, "title": "[claudinite-work] engine/update", "state": "open",
		"labels": []map[string]any{{"name": workitem.StatusRunningAgent}}, "body": taskspec.UpdateTaskPath + "\n"}
	item := writeJSONFile(t, dir, "item.json", held)
	comments := writeJSONFile(t, dir, "comments.json", []world.Comment{{ID: 1, Body: execute.HandoffComment("E1", "8-n")}})
	out, stderr, code := runInProc([]string{"work", "validate", "--repo", repo, "--issue", "8", "--nonce", "8-n", "--item-file", item, "--comments-file", comments}, "")
	if code != 0 {
		t.Fatal(stderr)
	}
	for _, s := range []string{"engine/update", "model: sonnet", workflows.StagingDir, "claudinite-ci.yml", "task:status:needs-human-decision"} {
		if !strings.Contains(out, s) {
			t.Errorf("lacks %q:\n%s", s, out)
		}
	}
}

func writeJSONFile(t *testing.T, dir, name string, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeRepoFile(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
