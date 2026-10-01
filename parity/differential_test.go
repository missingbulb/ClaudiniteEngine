package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// treesEnv names real repos (path-list separated) to sweep with both
// engines: each declares its packs in .claudinite-settings.json, as a
// member of the Node engine does.
const treesEnv = "CLAUDINITE_PARITY_TREES"

func TestDifferential(t *testing.T) {
	trees := filepath.SplitList(os.Getenv(treesEnv))
	if len(trees) == 0 {
		t.Skip(treesEnv + " is not set")
	}
	node := Node{Root: nodeRoot(t)}
	cn := Cn{Binary: cnBinary(t), Cache: t.TempDir()}
	for _, tree := range trees {
		tree := tree
		t.Run(filepath.Base(tree), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(tree, ".claudinite-settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			var decl map[string]any
			if err := json.Unmarshal(raw, &decl); err != nil {
				t.Fatal(err)
			}
			parent := t.TempDir()
			nodeDir, cnDir := filepath.Join(parent, "node"), filepath.Join(parent, "cn")
			for _, d := range []string{nodeDir, cnDir} {
				if out, err := exec.Command("git", "clone", "-q", "--no-hardlinks", tree, d).CombinedOutput(); err != nil {
					t.Fatalf("clone: %v %s", err, out)
				}
			}
			for _, d := range []string{nodeDir, cnDir} {
				if err := vendorPacks(d, tree, node.Root, decl); err != nil {
					t.Fatal(err)
				}
			}
			if err := cnSettings(cnDir, decl); err != nil {
				t.Fatal(err)
			}
			list, err := cn.List(cnDir)
			if err != nil {
				t.Fatal(err)
			}
			ids, listed := map[string]bool{}, map[string]Listed{}
			for _, l := range list {
				listed[l.ID] = l
				if l.Kind != "coded" && contains(l.Tags, "world") {
					ids[l.ID] = true
				}
			}
			declared, err := DeclaredIDs(nodeDir)
			if err != nil {
				t.Fatal(err)
			}
			nw, err := node.World(nodeDir)
			if err != nil {
				t.Fatal(err)
			}
			cw, err := cn.World(cnDir)
			if err != nil {
				t.Fatal(err)
			}
			subtracted := map[string]int{}
			for _, f := range nw {
				if !ids[f.Rule] {
					subtracted[f.Rule]++
				}
			}
			// A subtracted rule must be one cn knows it does not run, an
			// action check, or one it has never heard of, a Node coded
			// check; a declared check cn does not list failed to load.
			for r := range subtracted {
				l, ok := listed[r]
				switch {
				case ok && !contains(l.Tags, "action"):
					t.Errorf("subtracted %s, which cn lists as %s %v", r, l.Kind, l.Tags)
				case !ok && slices.Contains(declared, r):
					t.Errorf("subtracted %s, a declared check cn does not list: it did not load", r)
				}
			}
			kept := Keep(nw, ids)
			perRule := map[string]int{}
			for _, f := range kept {
				perRule[f.Rule]++
			}
			var counts []string
			for r, k := range perRule {
				counts = append(counts, fmt.Sprintf("%s %d", r, k))
			}
			sort.Strings(counts)
			t.Logf("%s: %d distinct rules found: %s", filepath.Base(tree), len(perRule), strings.Join(counts, ", "))
			onlyNode, onlyCn, agreed := diff(kept, Keep(cw, ids))
			for _, f := range onlyNode {
				t.Errorf("only node: %s", f)
			}
			for _, f := range onlyCn {
				t.Errorf("only cn: %s", f)
			}
			n := 0
			var rules []string
			for r, k := range subtracted {
				n += k
				rules = append(rules, fmt.Sprintf("%s %d", r, k))
			}
			sort.Strings(rules)
			t.Logf("%s: agreed %d, only-node %d, only-cn %d, subtracted %d (action, coded: %s)", filepath.Base(tree), agreed, len(onlyNode), len(onlyCn), n, strings.Join(rules, ", "))
		})
	}
}

// cnSettings writes cn's translation of the declaration, kept out of git.
func cnSettings(dir string, decl map[string]any) error {
	rel, err := Cn{}.Settings(dir, decl)
	if err != nil {
		return err
	}
	return appendFile(filepath.Join(dir, ".git/info/exclude"), "/"+rel+"\n")
}

// vendorPacks lays the declared canon packs where a member holds them,
// kept out of git, so both engines sweep the tree a member would have: a
// tree that is the canon itself takes them from its own packs/.
func vendorPacks(dir, tree, nodeRoot string, decl map[string]any) error {
	for _, id := range PackIDs(decl) {
		if strings.HasPrefix(id, "local/") {
			continue
		}
		dst := filepath.Join(dir, ".claudinite/shared/packs", id)
		if exists(dst) {
			continue
		}
		src := filepath.Join(tree, "packs", id)
		if !exists(src) {
			src = filepath.Join(nodeRoot, "packs", id)
		}
		if err := copyTree(src, dst); err != nil {
			return err
		}
	}
	return appendFile(filepath.Join(dir, ".git/info/exclude"), "/.claudinite/shared/\n")
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
