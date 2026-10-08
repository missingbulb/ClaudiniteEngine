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

// A member gets this engine's routine procedure at the path the routine's
// stored prompt names, replacing any older copy, with a cache .gitignore
// that keeps the copy out of the tree; a repo that is not a member gets
// neither.
func TestSessionStartWritesTheRoutineInstructionsInEveryMember(t *testing.T) {
	t.Parallel()
	repo := member(t, nil, nil)
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

	kept := member(t, nil, nil)
	put(t, kept, ".claudinite/cache/.gitignore", "/other\n")
	hook(t, Handler{ProjectDir: kept}, "session-start", startIn)
	hook(t, Handler{ProjectDir: kept}, "session-start", startIn)
	if got := read(t, kept, ".claudinite/cache/.gitignore"); got != "/other\n/instructions.md\n" {
		t.Errorf("a member's own .gitignore became %q", got)
	}

	none := t.TempDir()
	hook(t, Handler{ProjectDir: none}, "session-start", startIn)
	if _, err := os.Stat(filepath.Join(none, ".claudinite")); err == nil {
		t.Error("a repo that is not a member got a cache entry")
	}
}
