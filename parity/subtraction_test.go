package parity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtractionIsClosed(t *testing.T) {
	shelf := map[string]string{"old-check": "unported", "later-check": "ported", "forgotten": "ported"}
	deferred := map[string]string{"later-check": "task-runner"}
	ported := map[string]bool{"ported": true}
	listed := map[string]Listed{"listed-declared": {ID: "listed-declared", Kind: "declared"}}
	cases := []struct {
		rule, want string
	}{
		{"old-check", ""},
		{"later-check", ""},
		{"forgotten", "ported pack ported, which deferred.txt does not name"},
		{"nobody", "neither a coded check of an unported pack nor a line of deferred.txt"},
		{"listed-declared", "cn lists as declared"},
	}
	for _, c := range cases {
		got := unexplained(c.rule, listed, nil, shelf, deferred, ported)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.rule, got, c.want)
		}
	}
}

func TestDeferredNamesShelfRules(t *testing.T) {
	dir := t.TempDir()
	for rel, src := range map[string]string{
		"packs/a/worldRules/x.mjs":          "const rule = { id: 'x-check', on_fail: 'block' };",
		"packs/a/workRules/y.mjs":           "const rule = {\n  id: \"y-check\",\n};",
		"packs/b/skills/s/checks.mjs":       "export default [z];",
		"packs/b/skills/s/z.mjs":            "const rule = { id: 'z-check' };",
		"packs/b/test/worldRules/q.test.mjs": "const rule = { id: 'not-a-check' };",
	} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	shelf, err := ShelfChecks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"x-check": "a", "y-check": "a", "z-check": "b"}
	if len(shelf) != len(want) {
		t.Fatalf("shelf %v, want %v", shelf, want)
	}
	for id, p := range want {
		if shelf[id] != p {
			t.Errorf("shelf[%s] = %q, want %q", id, shelf[id], p)
		}
	}
	if bad := StrayDeferrals(map[string]string{"x-check": "verify", "gone": "adoption"}, shelf); len(bad) != 1 || bad[0] != "gone" {
		t.Errorf("stray deferrals %v, want [gone]", bad)
	}
}

func TestDeferredFileParses(t *testing.T) {
	d, err := Deferred()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"conformance-workflow", "task-declaration-shape", "pack-discovery-entry-await"} {
		if d[id] == "" {
			t.Errorf("deferred.txt does not name %s", id)
		}
	}
	if d["conformance-workflow"] != "verify" {
		t.Errorf("conformance-workflow is deferred to %q, want verify", d["conformance-workflow"])
	}
}
