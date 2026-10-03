package parity

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// The pack release's fingerprint validator and the engine's catalog
// reader are held to one corpus, the shelf's
// tools/release/testdata/detectors/*.json, each a manifest detector and
// the sentences the reader prints for it: the shelf's own test runs it
// through `cn fleet decide detector` when it has a cn, and this test
// always does over the ClaudinitePacks checkout CLAUDINITE_PACKS_TREE names.
func TestParityShelfDetectors(t *testing.T) {
	tree := os.Getenv(PacksTreeEnv)
	if tree == "" {
		t.Skip(PacksTreeEnv + " is not set; this run needs the ClaudinitePacks checkout")
	}
	dir := filepath.Join(tree, "tools", "release", "testdata", "detectors")
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 20 {
		t.Fatalf("%s holds %d detector fixtures", dir, len(names))
	}
	sort.Strings(names)
	cn := cnBinary(t)
	scratch := t.TempDir()
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var f struct {
			Detector json.RawMessage `json:"detector"`
			Expect   []string        `json:"expect"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		world := filepath.Join(scratch, "world.json")
		in, _ := json.Marshal(map[string]json.RawMessage{"detector": f.Detector})
		if err := os.WriteFile(world, in, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(cn, "fleet", "decide", "detector", "--world", world).Output()
		if err != nil {
			t.Fatalf("%s: cn fleet decide detector: %v", filepath.Base(name), err)
		}
		var got []string
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("%s: %v\n%s", filepath.Base(name), err, out)
		}
		if f.Expect == nil {
			f.Expect = []string{}
		}
		if !reflect.DeepEqual(got, f.Expect) {
			t.Errorf("%s: cn says %q, the shelf's fixture %q", filepath.Base(name), got, f.Expect)
		}
	}
}
