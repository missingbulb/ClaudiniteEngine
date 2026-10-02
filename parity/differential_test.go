package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// treesEnv names real repos (path-list separated) to sweep with both
// engines: each declares its packs in .claudinite-settings.json, as a
// member of the Node engine does. A tree that is not a git repo (the
// planted tree under testdata/trees) is committed as one first.
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
			src := tree
			if !exists(filepath.Join(tree, ".git")) {
				src = filepath.Join(parent, "src")
				if err := copyTree(tree, src); err != nil {
					t.Fatal(err)
				}
				if err := gitDo(src, "init", "-q", "-b", "main"); err != nil {
					t.Fatal(err)
				}
				if err := commitAll(src, "planted"); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range []string{nodeDir, cnDir} {
				if out, err := exec.Command("git", "clone", "-q", "--no-hardlinks", src, d).CombinedOutput(); err != nil {
					t.Fatalf("clone: %v %s", err, out)
				}
			}
			for d, engine := range map[string]string{nodeDir: "node", cnDir: "cn"} {
				if err := vendorPacks(d, tree, node.Root, engine, decl); err != nil {
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
			ported := Ported()
			shelf, err := ShelfChecks(node.Root)
			if err != nil {
				t.Fatal(err)
			}
			deferred, err := Explained()
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range StrayDeferrals(deferred, shelf) {
				t.Errorf("deferred.txt or testdata/answered names %s, which the frozen shelf does not carry", id)
			}
			for _, moment := range []string{"world", "work"} {
				// cn runs the declared and built-in checks, and the coded
				// checks of a ported pack; the rest of Node's coded
				// findings are subtracted.
				ids := map[string]bool{}
				for _, l := range list {
					if contains(l.Tags, moment) && (l.Kind == "declared" || l.Kind == "builtin" || l.Kind == "coded" && ported[l.Pack]) {
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
				compare(t, filepath.Base(tree)+" "+moment, nf, cf, ids, listed, declared, shelf, deferred)
			}
		})
	}
}

// compare asserts the two engines agree on the rules cn runs at one
// moment, and that every rule subtracted from Node's findings is one cn
// knows it does not run here (unexplained): a Node coded check of a pack
// not yet ported, or a rule deferred.txt names with the slice that ports
// it.
func compare(t *testing.T, label string, nf, cf []Finding, ids map[string]bool, listed map[string]Listed, declared []string, shelf, deferred map[string]string) {
	t.Helper()
	subtracted := map[string]int{}
	for _, f := range nf {
		if !ids[f.Rule] {
			subtracted[f.Rule]++
		}
	}
	ported := Ported()
	for r := range subtracted {
		if why := unexplained(r, listed, declared, shelf, deferred, ported); why != "" {
			t.Errorf("%s: %s", label, why)
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
// ported pack comes from ClaudinitePacks for cn; otherwise a tree that is
// the canon itself takes them from its own packs/.
func vendorPacks(dir, tree, nodeRoot, engine string, decl map[string]any) error {
	for _, id := range PackIDs(decl) {
		if strings.HasPrefix(id, "local/") {
			continue
		}
		dst := filepath.Join(dir, ".claudinite/shared/packs", id)
		if exists(dst) {
			continue
		}
		src := filepath.Join(tree, "packs", id)
		if engine == "cn" && Ported()[id] || !exists(src) {
			if err := VendorPack(id, filepath.Join(nodeRoot, "packs"), engine, dst); err != nil {
				return err
			}
			continue
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
