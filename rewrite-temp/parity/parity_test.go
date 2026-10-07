package parity

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	x := Expect{Rules: &a.Rules, Mounts: names(a.Mounts), Only: s.Expect.Only, LoaderOnly: s.Expect.LoaderOnly, Why: s.Expect.Why,
		Divergence: s.Expect.Divergence, CnWorld: s.Expect.CnWorld}
	if s.Expect.Skills != nil {
		x.Skills = &a.Skills
	}
	if !x.LoaderOnly {
		w, k := strs(a.World), strs(a.Work)
		x.World, x.Work = &w, &k
	}
	for _, ev := range hookEvents {
		var cases []HookCase
		for i, c := range *s.Expect.cases(ev.key) {
			v := a.Hooks[ev.key][i]
			cases = append(cases, HookCase{Payload: c.Payload, NoTranscript: c.NoTranscript, Exit: v.Exit, Block: v.Block(), Context: v.Context})
		}
		*x.cases(ev.key) = cases
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
	return build(t, "cn", "../../cn")
}

func decideBinary(t *testing.T) string {
	t.Helper()
	return build(t, "cndecide", "../cndecide")
}

func build(t *testing.T, name, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", name, err, out)
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
		es = append(es, Cn{Binary: cnBinary(t), Decide: decideBinary(t), Cache: t.TempDir()})
	}
	if es == nil {
		t.Fatalf("%s=%q: want node, cn or unset", engineEnv, sel)
	}
	return es
}

// Answer is what one engine said about one scenario.
type Answer struct {
	Rules, Skills, Flat string
	Mounts              map[string]string
	World, Work         []Finding
	// Hooks are the per-call answers, by expect.json key, case by case.
	Hooks map[string][]Verdict
}

// fired reports whether the answer holds any finding, block or context.
func (a Answer) fired() bool {
	if len(a.World)+len(a.Work) > 0 {
		return true
	}
	for _, vs := range a.Hooks {
		for _, v := range vs {
			if v.Exit != 0 || v.Context != "" {
				return true
			}
		}
	}
	return false
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
	dir, err := s.Materialize(memberDir(t), canon, e)
	if err != nil {
		t.Fatalf("%s: materialize: %v", e.Name(), err)
	}
	keep, err := s.Comparable(dir)
	if err != nil {
		t.Fatal(err)
	}
	transcript, err := s.Transcript(filepath.Dir(dir))
	if err != nil {
		t.Fatalf("%s: transcript: %v", e.Name(), err)
	}
	a := Answer{Hooks: map[string][]Verdict{}}
	// The sweeps run first: the session start writes files they would see.
	world, err := e.World(dir)
	if err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	a.World = Keep(world, keep)
	work, err := e.Work(dir, transcript)
	if err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	a.Work = Keep(work, keep)
	for _, ev := range hookEvents {
		for _, c := range *s.Expect.cases(ev.key) {
			payload, err := c.payload(ev.name, transcript)
			if err != nil {
				t.Fatal(err)
			}
			v, err := e.Hook(dir, ev.event, payload)
			if err != nil {
				t.Fatalf("%s %s: %v", e.Name(), ev.event, err)
			}
			a.Hooks[ev.key] = append(a.Hooks[ev.key], v)
		}
	}
	if a.Rules, err = e.Rules(dir); err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	if a.Skills, err = e.Skills(dir); err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	if a.Mounts, err = e.Mounts(dir); err != nil {
		t.Fatalf("%s: %v", e.Name(), err)
	}
	if a.Flat, err = e.Flat(dir); err != nil {
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
	if (x.Divergence == "") != (x.CnWorld == nil) || x.Divergence != "" && !divergenceForm.MatchString(x.Divergence) {
		t.Fatalf("%s/%s: a divergence names its record-<row> and carries cnWorld, both or neither", s.Group, s.Name)
	}
	if x.Divergence != "" && strings.HasPrefix(e, "cn") {
		if reflect.DeepEqual(*x.CnWorld, *x.World) {
			t.Errorf("%s/%s is marked %s but cn's world is the Node answer: drop the divergence", s.Group, s.Name, x.Divergence)
		}
		x.World = x.CnWorld
	}
	if strings.HasSuffix(s.Name, "fires") && !a.fired() {
		t.Errorf("%s: a fires scenario found nothing", e)
	}
	if strings.HasSuffix(s.Name, "silent") && a.fired() {
		t.Errorf("%s: a silent scenario found something", e)
	}
	for _, ev := range hookEvents {
		for i, c := range *x.cases(ev.key) {
			v := a.Hooks[ev.key][i]
			if v.Exit != c.Exit || v.Block() != c.Block || v.Context != c.Context {
				t.Errorf("%s %s case %d: exit %d block %q context %q\nwant exit %d block %q context %q\nstderr: %s",
					e, ev.event, i+1, v.Exit, v.Block(), v.Context, c.Exit, c.Block, c.Context, v.Stderr)
			}
		}
	}
	if x.Rules != nil && a.Rules != *x.Rules {
		t.Errorf("%s rules index:\n%s\nwant:\n%s", e, a.Rules, *x.Rules)
	}
	if x.Skills != nil && a.Skills != *x.Skills {
		t.Errorf("%s skills index:\n%s\nwant:\n%s", e, a.Skills, *x.Skills)
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

// agree holds the two engines' answers equal; a scenario marking a
// divergence has had each engine's world findings held to its own answer.
func agree(t *testing.T, s Scenario, a, b Answer) {
	t.Helper()
	if a.Rules != b.Rules {
		t.Errorf("rules index differs:\nnode:\n%s\ncn:\n%s", a.Rules, b.Rules)
	}
	if a.Skills != b.Skills {
		t.Errorf("skills index differs:\nnode:\n%s\ncn:\n%s", a.Skills, b.Skills)
	}
	if a.Flat != b.Flat {
		t.Errorf("flat declarations differ:\nnode:\n%s\ncn:\n%s", a.Flat, b.Flat)
	}
	if !reflect.DeepEqual(a.Mounts, b.Mounts) {
		t.Errorf("mounts differ: node %q, cn %q", names(a.Mounts), names(b.Mounts))
		for k, v := range a.Mounts {
			if w, ok := b.Mounts[k]; ok && v != w {
				t.Errorf("SKILL.md of %s differs", k)
			}
		}
	}
	for _, ev := range hookEvents {
		for i := range a.Hooks[ev.key] {
			x, y := a.Hooks[ev.key][i], b.Hooks[ev.key][i]
			if x.Exit != y.Exit || x.Block() != y.Block() || x.Context != y.Context {
				t.Errorf("%s case %d differs:\nnode exit %d block %q context %q\ncn   exit %d block %q context %q", ev.event, i+1, x.Exit, x.Block(), x.Context, y.Exit, y.Block(), y.Context)
			}
		}
	}
	for _, sc := range []struct {
		name string
		x, y []Finding
	}{{"world", a.World, b.World}, {"work", a.Work, b.Work}} {
		if sc.name == "world" && s.Expect.Divergence != "" {
			continue
		}
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
			if missing := s.UnvendoredPacks(); canon == "" && len(missing) > 0 {
				t.Skipf("declares canon packs %v, which come from the frozen engine %s names", missing, nodeEnv)
			}
			var answers []Answer
			for _, e := range es {
				if s.Expect.CnOnly && e.Name() != "cn" {
					t.Logf("parity %s/%s: cn only (%s)", s.Group, s.Name, s.Expect.Why)
					continue
				}
				a := ask(t, s, e, canon)
				if os.Getenv(recordEnv) == "1" && e.Name() == "node" {
					if err := record(s, a); err != nil {
						t.Fatal(err)
					}
					continue
				}
				check(t, s, e.Name(), a)
				answers = append(answers, a)
				hooks := 0
				for _, vs := range a.Hooks {
					hooks += len(vs)
				}
				t.Logf("parity %s/%s %s: world %d, work %d, mounts %d, hook calls %d", s.Group, s.Name, e.Name(), len(a.World), len(a.Work), len(a.Mounts), hooks)
			}
			if len(answers) == 2 {
				agree(t, s, answers[0], answers[1])
			}
		})
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

// memberDir is a scratch directory for one engine's copy of a scenario.
// Its removal is retried: a child an engine leaves behind (git's own
// background maintenance among them) can still be writing under .git
// when the test ends, which t.TempDir's cleanup reports as a failure.
func memberDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "parity-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("left %s behind", dir)
	})
	return dir
}
