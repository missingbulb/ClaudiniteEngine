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
			listed := map[string]Listed{}
			for _, l := range list {
				listed[l.ID] = l
			}
			declared, err := DeclaredIDs(nodeDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, moment := range []string{"world", "work"} {
				ids := map[string]bool{}
				for _, l := range list {
					if l.Kind != "coded" && contains(l.Tags, moment) {
						ids[l.ID] = true
					}
				}
				var nf, cf []Finding
				if moment == "world" {
					if nf, err = node.World(nodeDir); err == nil {
						cf, err = cn.World(cnDir)
					}
				} else {
					if nf, err = node.Work(nodeDir, ""); err == nil {
						cf, err = cn.Work(cnDir, "")
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				compare(t, filepath.Base(tree)+" "+moment, nf, cf, ids, listed, declared)
			}
		})
	}
}

// compare asserts the two engines agree on the rules cn runs at one
// moment, and that every rule subtracted from Node's findings is one cn
// knows it does not run here: a Node coded check, which cn lists as coded
// or has never heard of. A declared check cn does not list failed to
// load; one cn lists at another moment is a tagging fault.
func compare(t *testing.T, label string, nf, cf []Finding, ids map[string]bool, listed map[string]Listed, declared []string) {
	t.Helper()
	subtracted := map[string]int{}
	for _, f := range nf {
		if !ids[f.Rule] {
			subtracted[f.Rule]++
		}
	}
	for r := range subtracted {
		l, ok := listed[r]
		switch {
		case ok && l.Kind != "coded":
			t.Errorf("%s: subtracted %s, which cn lists as %s %v", label, r, l.Kind, l.Tags)
		case !ok && slices.Contains(declared, r):
			t.Errorf("%s: subtracted %s, a declared check cn does not list: it did not load", label, r)
		}
	}
	kept := Keep(nf, ids)
	perRule := map[string]int{}
	for _, f := range kept {
		perRule[f.Rule]++
	}
	var counts []string
	for r, k := range perRule {
		counts = append(counts, fmt.Sprintf("%s %d", r, k))
	}
	sort.Strings(counts)
	t.Logf("%s: %d distinct rules found: %s", label, len(perRule), strings.Join(counts, ", "))
	onlyNode, onlyCn, agreed := diff(kept, Keep(cf, ids))
	for _, f := range onlyNode {
		t.Errorf("%s: only node: %s", label, f)
	}
	for _, f := range onlyCn {
		t.Errorf("%s: only cn: %s", label, f)
	}
	n := 0
	var rules []string
	for r, k := range subtracted {
		n += k
		rules = append(rules, fmt.Sprintf("%s %d", r, k))
	}
	sort.Strings(rules)
	t.Logf("%s: agreed %d, only-node %d, only-cn %d, subtracted %d (coded: %s)", label, agreed, len(onlyNode), len(onlyCn), n, strings.Join(rules, ", "))
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
