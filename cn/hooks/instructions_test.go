package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
)

func read(t *testing.T, repo, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// A repo declaring the tasks pack gets this engine's routine procedure at
// the path the routine's stored prompt names, replacing any older copy,
// with a cache .gitignore that keeps the copy out of the tree; a repo
// declaring no tasks pack gets neither.
func TestSessionStartWritesTheRoutineInstructionsWhereTasksAreDeclared(t *testing.T) {
	t.Parallel()
	tasksPack := map[string]map[string]string{workitem.TasksPackID: {"pack.json": `{"version": "1", "minEngineVersion": "0.0.0"}`}}
	repo := member(t, []string{workitem.TasksPackID}, tasksPack)
	put(t, repo, workitem.InstructionsFile, "an older engine's copy\n")
	hook(t, Handler{ProjectDir: repo}, "session-start", startIn)
	if got := read(t, repo, workitem.InstructionsFile); got != workitem.RoutineInstructions {
		t.Errorf("instructions:\n%.200s", got)
	}
	if !strings.Contains(workitem.RoutinePrompt, workitem.InstructionsFile) {
		t.Errorf("the stored prompt %q does not name the file", workitem.RoutinePrompt)
	}
	ignore := read(t, repo, ".claudinite/cache/.gitignore")
	if !strings.Contains(ignore, "/instructions.md\n") || !strings.Contains(ignore, "/.gitignore\n") {
		t.Errorf(".gitignore %q", ignore)
	}

	kept := member(t, []string{workitem.TasksPackID}, tasksPack)
	put(t, kept, ".claudinite/cache/.gitignore", "/other\n")
	hook(t, Handler{ProjectDir: kept}, "session-start", startIn)
	hook(t, Handler{ProjectDir: kept}, "session-start", startIn)
	if got := read(t, kept, ".claudinite/cache/.gitignore"); got != "/other\n/instructions.md\n" {
		t.Errorf("a member's own .gitignore became %q", got)
	}

	none := member(t, []string{"alpha"}, map[string]map[string]string{"alpha": {"pack.json": `{"version": "1", "minEngineVersion": "0.0.0"}`}})
	hook(t, Handler{ProjectDir: none}, "session-start", startIn)
	if _, err := os.Stat(filepath.Join(none, ".claudinite/cache")); err == nil {
		t.Error("a repo without the tasks pack got a cache entry")
	}
}
