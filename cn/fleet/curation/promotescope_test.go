package curation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootsKeepTheShelfFirstAndNormalizeEachPath(t *testing.T) {
	got := Roots(map[string]any{"writePaths": []any{" skills ", "./prompts/", "packs", "", 7, "skills/"}})
	if want := "packs/ skills/ prompts/"; strings.Join(got, " ") != want {
		t.Errorf("Roots = %v, want %s", got, want)
	}
	if got := Roots(nil); strings.Join(got, " ") != "packs/" {
		t.Errorf("an unset config is the shelf alone, got %v", got)
	}
}

func TestRootsOfReadTheFleetBlock(t *testing.T) {
	got, err := RootsOf(t.TempDir())
	if err != nil || strings.Join(got, " ") != "packs/" {
		t.Errorf("a repo with no settings is the shelf alone, got %v, %v", got, err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claudinite"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claudinite", "settings.yaml"), []byte("fleet:\n  owner: acme\n  writePaths:\n    - skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = RootsOf(dir)
	if err != nil || strings.Join(got, " ") != "packs/ skills/" {
		t.Errorf("RootsOf = %v, %v", got, err)
	}
}

func TestStrayIsEveryPathOutsideTheRootsOnce(t *testing.T) {
	got := Stray([]string{"packs/", "skills/"}, []string{"packs/p/RULES.md", "engine/e.mjs", "skills/s.md", "packsx/a.md", "engine/e.mjs", "README.md"})
	if strings.Join(got, " ") != "README.md engine/e.mjs packsx/a.md" {
		t.Errorf("Stray = %v", got)
	}
}

// The gate scopes what the branch changed, deleted and left untracked
// since its merge base, and refuses a branch with none.
func TestCheckScopesTheBranchSinceItsMergeBase(t *testing.T) {
	f := newFixture(t)
	f.commit("base", map[string]string{"packs/p/RULES.md": "a\n", "README.md": "r\n", "engine/e.mjs": "e\n"})
	f.git("branch", "-M", "main")
	f.git("checkout", "-q", "-b", "claudinite/growth-promote-1")
	if err := os.Remove(filepath.Join(f.dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	f.commit("promote", map[string]string{"packs/p/RULES.md": "b\n", "engine/e.mjs": "e2\n"})
	if err := os.WriteFile(filepath.Join(f.dir, "stray.md"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Check(f.dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Roots, " ") != "packs/" || strings.Join(res.Stray, " ") != "README.md engine/e.mjs stray.md" {
		t.Errorf("Check = %+v", res)
	}
	f.git("checkout", "-q", "--orphan", "lonely")
	f.commit("alone", map[string]string{"packs/q/RULES.md": "q\n"})
	if _, err := Check(f.dir, "main"); !errors.Is(err, ErrNoMergeBase) {
		t.Errorf("a branch with no merge base is refused, got %v", err)
	}
}
