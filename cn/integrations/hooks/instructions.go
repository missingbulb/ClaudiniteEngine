package hooks

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// writeInstructions writes this engine's routine procedure where the
// routine's stored prompt reads it, and lists it in the cache directory's
// .gitignore so no commit ever pins an older copy. A .gitignore the engine
// writes ignores itself too; a member's own only gains the one line.
func writeInstructions(repo string) error {
	path := filepath.Join(repo, filepath.FromSlash(workitem.InstructionsFile))
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(workitem.RoutineInstructions), 0o644); err != nil {
		return err
	}
	line := "/" + filepath.Base(path) + "\n"
	ignore := filepath.Join(dir, ".gitignore")
	held, err := os.ReadFile(ignore)
	switch {
	case os.IsNotExist(err):
		return os.WriteFile(ignore, []byte(line+"/.gitignore\n"), 0o644)
	case err != nil:
		return err
	case strings.Contains("\n"+string(held), "\n"+line):
		return nil
	}
	if len(held) > 0 && !strings.HasSuffix(string(held), "\n") {
		held = append(held, '\n')
	}
	return os.WriteFile(ignore, append(held, line...), 0o644)
}
