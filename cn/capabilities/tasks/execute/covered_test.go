package execute

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/mergepolicy"
)

// A granular policy judges each path against what HEAD holds: a file HEAD
// has is modified, one it lacks is added.
func TestCoveredPathsReadTheChangeAgainstHead(t *testing.T) {
	root, _ := checkout(t)
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("changed\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "NEW.md"), []byte("new\n"), 0o644)
	w := worker(t)
	w.Place.Root = root
	w.Log = func(string) {}
	w.Rules = mergepolicy.Compile([]mergepolicy.PackRules{{ID: "acme-pack", File: "merge-rules.json", Specs: []any{map[string]any{
		"name": "acme-edits", "pathMatching": `/\.md$/`, "changeKinds": []any{"modified"}, "editShape": "any"}}}})
	tk := shellTask(t, "true", map[string]any{"automerge": []any{"acme-edits"}})
	if got := w.coveredPaths(tk, []string{"NEW.md", "README.md"}); !reflect.DeepEqual(got, []string{"README.md"}) {
		t.Errorf("covered %v, want the modified README.md alone", got)
	}
}
