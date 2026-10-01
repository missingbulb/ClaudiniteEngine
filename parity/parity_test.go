package parity

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The harness runs only when CLAUDINITE_NODE_ENGINE names the frozen
// Node engine's checkout, which is also where canon packs come from.
// CLAUDINITE_PARITY_ENGINE narrows the run to node or cn; unset runs
// both and compares them. CLAUDINITE_CN names a built cn; unset builds
// one.
const (
	nodeEnv   = "CLAUDINITE_NODE_ENGINE"
	engineEnv = "CLAUDINITE_PARITY_ENGINE"
	cnEnv     = "CLAUDINITE_CN"
	// recordEnv, set to 1, writes the Node engine's answers into each
	// scenario's expect.json instead of asserting them: a scenario is
	// written against the Node engine first.
	recordEnv = "CLAUDINITE_PARITY_RECORD"
)

func record(s Scenario, a Answer) error {
	x := Expect{Rules: &a.Rules, Mounts: names(a.Mounts), Only: s.Expect.Only, LoaderOnly: s.Expect.LoaderOnly}
	if !x.LoaderOnly {
		w, k := strs(a.World), strs(a.Work)
		x.World, x.Work = &w, &k
	}
	raw, err := jsonIndent(x)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, "expect.json"), raw, 0o644)
}

func nodeRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv(nodeEnv)
	if root == "" {
		t.Skip(nodeEnv + " is not set; this run needs the frozen Node engine")
	}
	return root
}

func cnBinary(t *testing.T) string {
	t.Helper()
	if b := os.Getenv(cnEnv); b != "" {
		return b
	}
	bin := filepath.Join(t.TempDir(), "cn")
	cmd := exec.Command("go", "build", "-o", bin, "../cmd/cn")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build cn: %v\n%s", err, out)
	}
	return bin
}

// engines are the engines CLAUDINITE_PARITY_ENGINE names: both by default
// when the Node engine is present, cn alone when it is not.
func engines(t *testing.T) []Engine {
	t.Helper()
	root := os.Getenv(nodeEnv)
	sel := os.Getenv(engineEnv)
	if sel == "" {
		sel = "both"
		if root == "" {
			sel = "cn"
		}
	}
	var es []Engine
	if sel == "node" || sel == "both" {
		es = append(es, Node{Root: nodeRoot(t)})
	}
	if sel == "cn" || sel == "both" {
		es = append(es, Cn{Binary: cnBinary(t), Cache: t.TempDir()})
	}
	if es == nil {
		t.Fatalf("%s=%q: want node, cn or unset", engineEnv, sel)
	}
	return es
}

// Answer is what one engine said about one scenario.
type Answer struct {
	Rules       string
	Mounts      map[string]string
	World, Work []Finding
}

func names(m map[string]string) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func strs(fs []Finding) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

func ask(t *testing.T, s Scenario, e Engine, canon string) Answer {
	t.Helper()
	dir, err := s.Materialize(t.TempDir(), canon, e)
	if err != nil {
		t.Fatalf("%s: materialize: %v", e.Name(), err)
	}
	keep, err := s.Comparable(dir)
	if err != nil {
		t.Fatal(err)
	}
	var a Answer
	// The sweeps run first: the session start writes files they would see.
	world, err := e.World(dir)
	if err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	a.World = Keep(world, keep)
	work, err := e.Work(dir)
	if err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	a.Work = Keep(work, keep)
	if a.Rules, err = e.Rules(dir); err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	if a.Mounts, err = e.Mounts(dir); err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	return a
}

func check(t *testing.T, s Scenario, e string, a Answer) {
	t.Helper()
	if e == "node" {
		// The Node engine is the reference: its miss is the scenario's.
		e = "node (a scenario that fails on the Node engine is a wrong scenario, not a cn bug)"
	}
	x := s.Expect
	if x.World != nil && x.Work != nil {
		n := len(*x.World) + len(*x.Work)
		if s.Name == "fires" && n == 0 {
			t.Errorf("a fires scenario expects no finding")
		}
		if s.Name == "silent" && n != 0 {
			t.Errorf("a silent scenario expects findings")
		}
	}
	if x.Rules != nil && a.Rules != *x.Rules {
		t.Errorf("%s rules index:\n%s\nwant:\n%s", e, a.Rules, *x.Rules)
	}
	if x.Mounts != nil && !reflect.DeepEqual(names(a.Mounts), x.Mounts) {
		t.Errorf("%s mounts %q, want %q", e, names(a.Mounts), x.Mounts)
	}
	if x.World != nil && !reflect.DeepEqual(strs(a.World), *x.World) {
		t.Errorf("%s world:\n  %s\nwant:\n  %s", e, strings.Join(strs(a.World), "\n  "), strings.Join(*x.World, "\n  "))
	}
	if x.Work != nil && !reflect.DeepEqual(strs(a.Work), *x.Work) {
		t.Errorf("%s work:\n  %s\nwant:\n  %s", e, strings.Join(strs(a.Work), "\n  "), strings.Join(*x.Work, "\n  "))
	}
}

func agree(t *testing.T, a, b Answer) {
	t.Helper()
	if a.Rules != b.Rules {
		t.Errorf("rules index differs:\nnode:\n%s\ncn:\n%s", a.Rules, b.Rules)
	}
	if !reflect.DeepEqual(a.Mounts, b.Mounts) {
		t.Errorf("mounts differ: node %q, cn %q", names(a.Mounts), names(b.Mounts))
		for k, v := range a.Mounts {
			if w, ok := b.Mounts[k]; ok && v != w {
				t.Errorf("SKILL.md of %s differs", k)
			}
		}
	}
	for _, sc := range []struct {
		name string
		x, y []Finding
	}{{"world", a.World, b.World}, {"work", a.Work, b.Work}} {
		onlyNode, onlyCn, _ := diff(sc.x, sc.y)
		for _, f := range onlyNode {
			t.Errorf("%s: only node: %s", sc.name, f)
		}
		for _, f := range onlyCn {
			t.Errorf("%s: only cn: %s", sc.name, f)
		}
	}
}

func TestParity(t *testing.T) {
	es := engines(t)
	canon := ""
	if root := os.Getenv(nodeEnv); root != "" {
		canon = filepath.Join(root, "packs")
	}
	scenarios, err := LoadScenarios("testdata/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("no scenarios")
	}
	for _, s := range scenarios {
		s := s
		t.Run(s.Group+"/"+s.Name, func(t *testing.T) {
			if canon == "" && len(s.CanonPacks()) > 0 {
				t.Skipf("declares canon packs %v, which come from the frozen engine %s names", s.CanonPacks(), nodeEnv)
			}
			var answers []Answer
			for _, e := range es {
				a := ask(t, s, e, canon)
				if os.Getenv(recordEnv) == "1" && e.Name() == "node" {
					if err := record(s, a); err != nil {
						t.Fatal(err)
					}
					continue
				}
				check(t, s, e.Name(), a)
				answers = append(answers, a)
				t.Logf("parity %s/%s %s: world %d, work %d, mounts %d", s.Group, s.Name, e.Name(), len(a.World), len(a.Work), len(a.Mounts))
			}
			if len(answers) == 2 {
				agree(t, answers[0], answers[1])
			}
		})
	}
}

func TestTranslate(t *testing.T) {
	got, err := Translate(map[string]any{
		"packs":  []any{"basics", map[string]any{"id": "node", "version": "1", "config": map[string]any{"x": 1.0}}, map[string]any{"id": "local/mine", "via": "x"}},
		"rules":  map[string]any{"acme-check": "off"},
		"accept": []any{map[string]any{"rule": "acme-check", "reason": "r"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version": "0.0.0"`, `"basics"`, `"id": "node"`, `"local/mine"`, `"acme-check": "off"`, `"reason": "r"`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	for _, gone := range []string{`"via"`, `"version": "1"`} {
		if strings.Contains(string(got), gone) {
			t.Errorf("kept %s in\n%s", gone, got)
		}
	}
}

func TestParsers(t *testing.T) {
	node := parseNode("[BLOCKING] acme-check  a.md:3\n  why\n[ADVISORY] config  .claudinite-settings.json\n")
	cn := parseCn("finding acme-pack/acme-check a.md:3: x\n  why: y\nadvisory config .claudinite/settings.json: z\n")
	if !reflect.DeepEqual(node, cn) || len(node) != 2 || node[1].Path != SettingsPath {
		t.Errorf("node %v, cn %v", node, cn)
	}
}

func TestYAML(t *testing.T) {
	got, err := yamlDoc(map[string]any{"b": []any{"x", map[string]any{"k": true, "n": 1.5}, []any{}}, "a": map[string]any{}, "c": "yes"})
	want := `"a": {}
"b":
  - "x"
  -
    "k": true
    "n": 1.5
  - []
"c": "yes"
`
	if err != nil || got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
