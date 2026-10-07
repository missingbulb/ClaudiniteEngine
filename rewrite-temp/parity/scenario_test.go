package parity

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Every diverged path names its record row and still differs between the
// frozen shelf and the ClaudinitePacks tree: one that converged back is
// dropped from the list.
func TestDivergedPathsStillDiffer(t *testing.T) {
	diverged, err := Diverged()
	if err != nil {
		t.Fatal(err)
	}
	node, tree := os.Getenv(nodeEnv), os.Getenv(PacksTreeEnv)
	if node == "" || tree == "" {
		t.Skipf("%s and %s name the two shelves", nodeEnv, PacksTreeEnv)
	}
	for p := range diverged {
		pack, _, _ := strings.Cut(p, "/")
		if !Ported()[pack] {
			t.Errorf("%s: %s is not a ported pack", p, pack)
			continue
		}
		a, b := filepath.Join(node, "packs", p), filepath.Join(tree, "packs", p)
		if sameTree(t, a, b) {
			t.Errorf("%s no longer differs from the frozen shelf: drop it from diverged.txt", p)
		}
	}
}

func sameTree(t *testing.T, a, b string) bool {
	t.Helper()
	read := func(root string) map[string]string {
		out := map[string]string{}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			raw, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(root, p)
			out[rel] = string(raw)
			return nil
		})
		return out
	}
	return reflect.DeepEqual(read(a), read(b))
}

// The parity README counts the scenarios face's divergences.
func TestScenarioDivergencesAreCounted(t *testing.T) {
	scenarios, err := LoadScenarios("testdata/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range scenarios {
		if s.Expect.Divergence != "" {
			n++
		}
	}
	countedIn(t, "Scenarios", n, len(scenarios))
}
