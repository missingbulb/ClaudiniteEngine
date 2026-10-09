package skilltriggers

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/skills/skillfm"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
)

func TestGlob(t *testing.T) {
	cases := []struct {
		glob string
		hit  []string
		miss []string
	}{
		{"docs/**", []string{"docs/a", "docs/a/b.md"}, []string{"docs", "x/docs/a"}},
		{"**/packs/*/RULES.md", []string{"packs/a/RULES.md", "x/y/packs/a/RULES.md"}, []string{"packs/a/b/RULES.md"}},
		{"src/*.go", []string{"src/x.go"}, []string{"src/a/x.go", "src/x.gox"}},
		{"cfg/{a,b}.json", []string{"cfg/a.json", "cfg/b.json"}, []string{"cfg/c.json"}},
		{"a/{x,{y,z}}/f?.md", []string{"a/x/f1.md", "a/z/f2.md"}, []string{"a/w/f1.md", "a/x/f/.md"}},
		{"(odd)+[x].md", []string{"(odd)+[x].md"}, []string{"odd.md"}},
	}
	for _, c := range cases {
		re, err := Glob(c.glob)
		if err != nil {
			t.Fatalf("%s: %v", c.glob, err)
		}
		for _, p := range c.hit {
			if !re.Test(p) {
				t.Errorf("%s should match %s", c.glob, p)
			}
		}
		for _, p := range c.miss {
			if re.Test(p) {
				t.Errorf("%s should not match %s", c.glob, p)
			}
		}
	}
}

func TestParseTool(t *testing.T) {
	cases := []struct {
		entry              string
		ok                 bool
		tool, field, toolR string
		pattern            string
	}{
		{"mcp__github__create_pull_request", true, "mcp__github__create_pull_request", "", "", ""},
		{`Bash.command /(^|[;&|]\s*)git\s+commit\b/`, true, "Bash", "command", "", `(^|[;&|]\s*)git\s+commit\b`},
		{`/^mcp__acme__/`, true, "", "", "^mcp__acme__", ""},
		{`WebFetch /EGRESS_BLOCKED|\b403\b/`, true, "WebFetch", "", "", `EGRESS_BLOCKED|\b403\b`},
		{`a.b.c /x/`, true, "a", "b.c", "", "x"},
		{`Bash /unclosed(/`, false, "", "", "", ""},
		{`Bash not-a-regex`, false, "", "", "", ""},
		{`/bad(/`, false, "", "", "", ""},
	}
	for _, c := range cases {
		tr, ok := ParseTool(c.entry)
		if ok != c.ok {
			t.Errorf("%q: ok %v", c.entry, ok)
			continue
		}
		if !ok {
			continue
		}
		toolR, pat := "", ""
		if tr.ToolRe != nil {
			toolR = tr.ToolRe.Source
		}
		if tr.Pattern != nil {
			pat = tr.Pattern.Source
		}
		if tr.Tool != c.tool || tr.Field != c.field || toolR != c.toolR || pat != c.pattern || tr.Source != c.entry {
			t.Errorf("%q: %+v", c.entry, tr)
		}
	}
}

func val(t *testing.T, s string) jsjson.Value {
	t.Helper()
	v, err := jsjson.Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHitsAndMissing(t *testing.T) {
	m := skillfm.Read("---\nname: g\nmetadata:\n  force-load-on-file-edits-paths: ['docs/**']\n  force-load-on-tool-calls:\n    - 'Bash.command /deploy/'\n    - 'WebFetch'\n    - 'Grep /\"glob\"/'\n    - 'Bash /oops('\n  force-load-on-prompts-matching:\n    - '/SHIP IT/i'\n    - 'no slashes'\n  force-load-on-tool-results-matching:\n    - 'Bash /EGRESS/'\n    - 'Read'\n---\n")
	ts, bad := Of("p", "g", "/r/x/skills/g", m)
	if len(ts) != 7 || len(bad) != 2 {
		t.Fatalf("%d triggers, %d malformed: %+v", len(ts), len(bad), bad)
	}
	if bad[0].Entry != "Bash /oops(" || bad[1].Entry != "no slashes" || bad[1].Key != skillfm.ForceLoadPrompts {
		t.Errorf("%+v", bad)
	}
	call := func(tool, input string) Call { return Call{Tool: tool, Input: val(t, input), HasInput: true} }
	calls := 0
	loaded := func() map[string]bool { calls++; return map[string]bool{} }
	if got := Missing(ts, loaded, func(tr Trigger) bool { return tr.HitsCall(call("Bash", `{"command":"ls"}`)) }); len(got) != 0 || calls != 0 {
		t.Errorf("a call no trigger names read the transcript: %v %d", got, calls)
	}
	got := Missing(ts, loaded, func(tr Trigger) bool { return tr.HitsCall(call("Bash", `{"command":"make deploy"}`)) })
	if len(got) != 1 || got[0].Source != "Bash.command /deploy/" || calls != 1 {
		t.Errorf("%+v", got)
	}
	if got := Missing(ts, loaded, func(tr Trigger) bool { return tr.HitsCall(call("Grep", `{"pattern":"p","glob":"x"}`)) }); len(got) != 1 {
		t.Errorf("whole input: %+v", got)
	}
	if got := Missing(ts, func() map[string]bool { return map[string]bool{"g": true} }, func(tr Trigger) bool { return tr.HitsPath("docs/a.md") }); len(got) != 0 {
		t.Error("a loaded skill is never asked for again")
	}
	if got := Missing(ts, loaded, func(tr Trigger) bool { return tr.HitsPrompt("please ship it") }); len(got) != 1 {
		t.Error("prompt")
	}
	res := Call{Tool: "Read", Response: val(t, `"EGRESS"`), HasResponse: true}
	if got := Missing(ts, loaded, func(tr Trigger) bool { return tr.HitsResult(res) }); len(got) != 0 {
		t.Error("a result trigger with no pattern matches nothing")
	}
	res.Tool = "Bash"
	if got := Missing(ts, loaded, func(tr Trigger) bool { return tr.HitsResult(res) }); len(got) != 1 {
		t.Error("result")
	}
	both := append(append([]Trigger{}, ts...), Trigger{Pack: "q", Skill: "h", Dir: "/elsewhere/h", Kind: Path, Pattern: ts[0].Pattern, Source: "docs/**"})
	ms := Missing(both, loaded, func(tr Trigger) bool { return tr.HitsPath("docs/a.md") })
	if got := LoadInstruction(ms, "/r"); got != `Skill tool, skill: "g", skill: "h", or Read x/skills/g/SKILL.md or /elsewhere/h/SKILL.md` {
		t.Error(got)
	}
	if Skills(ms) != "`g` and `h`" || Packs(ms) != "p, q" || Sources(ms) != "docs/**, docs/**" {
		t.Error(Skills(ms), Packs(ms), Sources(ms))
	}
}

// The glob grammar against the Node engine's globToRegExp, when the
// frozen engine is at hand.
func TestGlobAgainstNode(t *testing.T) {
	root := os.Getenv("CLAUDINITE_NODE_ENGINE")
	if root == "" {
		t.Skip("CLAUDINITE_NODE_ENGINE is not set")
	}
	globs := []string{"docs/**", "**/x/*.md", "a/{b,c{d,e}}/**/f?", "*.{js,mjs}", "**", "a**b", "x/**/"}
	paths := []string{"docs/a", "x/y.md", "q/x/y.md", "a/b/f1", "a/cd/z/f2", "a/ce/f3", "m.js", "n.mjs", "a/b", "ab", "aXb", "a/x/b", "x/", "x/y/"}
	script := `const m = await import(process.argv[1]); const gs = JSON.parse(process.argv[2]); const ps = JSON.parse(process.argv[3]);
process.stdout.write(JSON.stringify(gs.map((g) => ps.map((p) => m.globToRegExp(g).test(p)))));`
	gj, _ := json.Marshal(globs)
	pj, _ := json.Marshal(paths)
	out, err := exec.Command("node", "--input-type=module", "-e", script, filepath.Join(root, "engine/pack_loader/path-scoped-skills.mjs"), string(gj), string(pj)).Output()
	if err != nil {
		t.Fatal(err)
	}
	var node [][]bool
	if err := json.Unmarshal(out, &node); err != nil {
		t.Fatal(err)
	}
	var mine [][]bool
	for _, g := range globs {
		re, err := Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		var row []bool
		for _, p := range paths {
			row = append(row, re.Test(p))
		}
		mine = append(mine, row)
	}
	if !reflect.DeepEqual(mine, node) {
		t.Errorf("go %v\nnode %v", mine, node)
	}
}

// FromPacks reads each pack's skills; under packset.Memoize the first
// reading stands for the process.
func TestFromPacksMemoized(t *testing.T) {
	defer packset.Forget()
	dir := t.TempDir()
	skill := filepath.Join(dir, "skills", "guide", "SKILL.md")
	_ = os.MkdirAll(filepath.Dir(skill), 0o755)
	write := func(glob string) {
		_ = os.WriteFile(skill, []byte("---\nname: guide\nmetadata:\n  force-load-on-file-edits-paths:\n    - \""+glob+"\"\n---\n"), 0o644)
	}
	packs := []packset.Pack{{ID: "p", Dir: dir, Skills: []string{"guide"}}}
	source := func() string {
		ts, _ := FromPacks(packs)
		if len(ts) != 1 {
			t.Fatalf("%+v", ts)
		}
		return ts[0].Source
	}
	write("a/**")
	if source() != "a/**" {
		t.Fatal(source())
	}
	write("b/**")
	if source() != "b/**" {
		t.Error("an unmemoized read kept the old trigger")
	}
	packset.Memoize()
	_ = source()
	write("c/**")
	if source() != "b/**" {
		t.Error("a memoized read read the skill again")
	}
}
