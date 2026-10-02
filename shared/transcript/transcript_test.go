package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func line(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func user(text any) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}}
}

func assistant(blocks ...map[string]any) map[string]any {
	l := []any{}
	for _, b := range blocks {
		l = append(l, b)
	}
	return map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": l}}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func toolUse(id, name string, input any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResult(id string, isError bool, content any) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isError, "content": content}}}}
}

// write lays a session out as Claude Code does: <dir>/session.jsonl and
// <dir>/session/subagents/agent-<id>.jsonl.
func write(t *testing.T, entries []map[string]any, subagents map[string][]map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	jsonl := func(es []map[string]any) string {
		var b strings.Builder
		for _, e := range es {
			b.WriteString(line(t, e) + "\n")
		}
		return b.String()
	}
	p := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(p, []byte(jsonl(entries)), 0o644); err != nil {
		t.Fatal(err)
	}
	for id, es := range subagents {
		d := filepath.Join(dir, "session", "subagents")
		_ = os.MkdirAll(d, 0o755)
		if err := os.WriteFile(filepath.Join(d, "agent-"+id+".jsonl"), []byte(jsonl(es)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestEntriesSkipsPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(`{"type":"user","message":{"content":"hi"}}`+"\n\n"+`{"type":"assis`), 0o644)
	es := Entries(p)
	if len(es) != 1 || es[0].Type != "user" {
		t.Fatalf("%+v", es)
	}
	if Entries(filepath.Join(t.TempDir(), "absent.jsonl")) != nil {
		t.Error("absent file")
	}
}

func TestOwnerTurns(t *testing.T) {
	p := write(t, []map[string]any{
		user("please fix it"),
		user("<system-reminder>not the owner</system-reminder>"),
		user("  <command-message>x</command-message>"),
		{"type": "user", "isMeta": true, "message": map[string]any{"content": "meta"}},
		{"type": "user", "isSidechain": true, "message": map[string]any{"content": "a subagent's prompt"}},
		user([]any{text("block one"), text("block two")}),
		user([]any{text("text"), map[string]any{"type": "image"}}),
		user(""),
		user("[Request interrupted by user for tool use]"),
		toolResult("t1", false, "result"),
	}, nil)
	var got []string
	for _, turn := range OwnerTurns(Entries(p)) {
		got = append(got, turn.Text)
	}
	// The interruption marker reads as an owner turn, as in the Node engine
	// (the canon rule writing-check-reads holds of both engines).
	want := []string{"please fix it", "block one\nblock two", "[Request interrupted by user for tool use]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestReplyClasses(t *testing.T) {
	p := write(t, []map[string]any{
		user("first"),
		assistant(text("Comment class: feature\nworking")),
		user("second"),
		assistant(text("> **Comment class**: Process change, and other")),
		user("third"),
		assistant(text("no class here")),
	}, nil)
	es := Entries(p)
	turns := ClassifiedTurns(es)
	if len(turns) != 3 {
		t.Fatal(len(turns))
	}
	if !reflect.DeepEqual(sorted(turns[0].Classes), []string{"feature"}) || !reflect.DeepEqual(sorted(turns[1].Classes), []string{"other", "process-change"}) || len(turns[2].Classes) != 0 {
		t.Errorf("%+v", turns)
	}
	if got := sorted(ReplyClasses(es)); !reflect.DeepEqual(got, []string{"feature", "other", "process-change"}) {
		t.Errorf("%v", got)
	}
	if ClassificationLine("x\n  comment class (mixed): correction\ny") != "  comment class (mixed): correction" {
		t.Error("line")
	}
}

func sorted(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestToolCallsAndDenials(t *testing.T) {
	p := write(t, []map[string]any{
		assistant(toolUse("a", "Bash", json.RawMessage(`{"command":"HELLO_GUARD","z":1,"b":2}`)), toolUse("b", "Read", nil)),
		toolResult("a", true, "Blocked by hello-guard: no\nBlocked by other.rule: also\n[cn] hooks pre-tool-use block 3ms"),
		toolResult("b", true, []any{text("Blocked by x: in a block")}),
		{"type": "assistant", "isSidechain": true, "message": map[string]any{"content": []any{toolUse("c", "Edit", map[string]any{"file_path": "f"})}}},
		assistant(map[string]any{"type": "tool_use", "id": "d"}),
	}, nil)
	calls := ToolCalls(SessionEntries(Paths(p)))
	if len(calls) != 3 {
		t.Fatalf("%+v", calls)
	}
	if calls[0].Name != "Bash" || calls[0].InputJSON() != `{"command":"HELLO_GUARD","z":1,"b":2}` || !reflect.DeepEqual(calls[0].DeniedBy, []string{"hello-guard", "other.rule"}) {
		t.Errorf("%+v %s", calls[0], calls[0].InputJSON())
	}
	if calls[1].InputJSON() != `{}` || !reflect.DeepEqual(calls[1].DeniedBy, []string{"x"}) {
		t.Errorf("%+v", calls[1])
	}
	if !calls[2].Sidechain || calls[2].DeniedBy != nil {
		t.Errorf("%+v", calls[2])
	}
}

func TestSkillLoadsAcrossSubagents(t *testing.T) {
	p := write(t, []map[string]any{
		assistant(toolUse("a", "Skill", map[string]any{"skill": "do-later"})),
		assistant(toolUse("b", "Read", map[string]any{"file_path": "/repo/.claude/skills/committing/SKILL.md"})),
		assistant(toolUse("c", "Read", map[string]any{"file_path": "/repo/skills/x/README.md"})),
		user("<command-name>/merge-to-main</command-name> and <command-name>plugin:thing</command-name>"),
	}, map[string][]map[string]any{
		"b2": {assistant(toolUse("e", "Skill", map[string]any{"skill": "late"}))},
		"a1": {user("a subagent prompt"), assistant(toolUse("d", "Skill", map[string]any{"skill": "delegated"}))},
	})
	paths := Paths(p)
	if len(paths) != 3 || !strings.HasSuffix(paths[1], "agent-a1.jsonl") {
		t.Fatalf("%v", paths)
	}
	got := SkillLoads(SessionEntries(paths))
	want := []string{"do-later", "committing", "merge-to-main", "plugin:thing", "delegated", "late"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if n := len(OwnerTurns(Entries(p))); n != 0 {
		t.Errorf("owner turns from the session file alone: %d", n)
	}
	if Paths("") != nil {
		t.Error("no path")
	}
}

// A session reads its files at most twice: the loads-only read, then the
// full one when a verdict needs the calls; after the full read the loads
// come from it.
func TestSessionReadsOnce(t *testing.T) {
	p := write(t, []map[string]any{assistant(toolUse("a", "Skill", map[string]any{"skill": "s"}))}, nil)
	s := NewSession(p)
	if !s.Loaded()["s"] {
		t.Fatal("not loaded")
	}
	if s.all != nil || s.calls != nil {
		t.Error("the loads read parsed the whole session")
	}
	if len(s.Calls()) != 1 {
		t.Fatal("no calls")
	}
	_ = os.Remove(p)
	if !s.Loaded()["s"] || len(s.Calls()) != 1 {
		t.Error("read again")
	}
	s = NewSession(write(t, []map[string]any{assistant(toolUse("a", "Skill", map[string]any{"skill": "s"}))}, nil))
	_ = s.Calls()
	_ = os.Remove(s.Path)
	if !s.Loaded()["s"] {
		t.Error("the loads after a full read read the files again")
	}
	var none *Session
	if len(none.Loaded()) != 0 || none.Calls() != nil || len(none.ReplyClasses()) != 0 {
		t.Error("nil session")
	}
}

// A long synthetic session read by both engines' readers, when the frozen
// Node engine is at hand: the same owner turns, classes, calls, denials and
// loads.
func TestAgainstNodeReader(t *testing.T) {
	root := os.Getenv("CLAUDINITE_NODE_ENGINE")
	if root == "" {
		t.Skip("CLAUDINITE_NODE_ENGINE is not set")
	}
	var es []map[string]any
	for i := 0; i < 3000; i++ {
		switch i % 7 {
		case 0:
			es = append(es, user(fmt.Sprintf("turn %d", i)))
		case 1:
			es = append(es, assistant(text(fmt.Sprintf("Comment class: %s", []string{"feature", "correction", "process change", "other"}[i%4])), toolUse(fmt.Sprintf("u%d", i), "Bash", map[string]any{"command": fmt.Sprintf("echo %d", i), "n": i})))
		case 2:
			es = append(es, toolResult(fmt.Sprintf("u%d", i-1), i%3 == 0, fmt.Sprintf("Blocked by r%d: x", i%5)))
		case 3:
			es = append(es, user("<system-reminder>r</system-reminder>"))
		case 4:
			es = append(es, assistant(toolUse(fmt.Sprintf("s%d", i), "Skill", map[string]any{"skill": fmt.Sprintf("k%d", i%11)})))
		case 5:
			es = append(es, user(fmt.Sprintf("<command-name>/c%d</command-name>", i%13)))
		case 6:
			es = append(es, map[string]any{"type": "assistant", "isSidechain": true, "message": map[string]any{"content": []any{toolUse(fmt.Sprintf("r%d", i), "Read", map[string]any{"file_path": fmt.Sprintf("/x/skills/r%d/SKILL.md", i%3)})}}})
		}
	}
	p := write(t, es, map[string][]map[string]any{"z": {assistant(toolUse("sub", "Skill", map[string]any{"skill": "sub"}))}})
	script := `const t = await import(process.argv[1]);
const fs = await import('node:fs');
const paths = t.sessionTranscriptPaths(process.argv[2]);
const all = paths.flatMap((p) => t.parseEntries(fs.readFileSync(p, 'utf8')));
const own = t.parseEntries(fs.readFileSync(process.argv[2], 'utf8'));
const classes = new Set(); for (const c of t.classifiedTurns(own)) for (const k of c.classes) classes.add(k);
process.stdout.write(JSON.stringify({
  turns: t.humanTurns(own).map((x) => x.text),
  classes: [...classes].sort(),
  calls: t.toolCalls(all).map((c) => [c.name, JSON.stringify(c.input), c.sidechain, c.deniedBy]),
  loads: t.skillLoads(all),
}));`
	out, err := exec.Command("node", "--input-type=module", "-e", script, filepath.Join(root, "engine/checks/helpers/session-transcript.mjs"), p).Output()
	if err != nil {
		t.Fatal(err)
	}
	var node struct {
		Turns   []string `json:"turns"`
		Classes []string `json:"classes"`
		Calls   [][]any  `json:"calls"`
		Loads   []string `json:"loads"`
	}
	if err := json.Unmarshal(out, &node); err != nil {
		t.Fatal(err)
	}
	own := Entries(p)
	all := SessionEntries(Paths(p))
	var turns []string
	for _, x := range OwnerTurns(own) {
		turns = append(turns, x.Text)
	}
	if !reflect.DeepEqual(turns, node.Turns) {
		t.Errorf("turns differ: %d vs %d", len(turns), len(node.Turns))
	}
	if got := sorted(ReplyClasses(own)); !reflect.DeepEqual(got, node.Classes) {
		t.Errorf("classes %v vs %v", got, node.Classes)
	}
	calls := ToolCalls(all)
	if len(calls) != len(node.Calls) {
		t.Fatalf("calls %d vs %d", len(calls), len(node.Calls))
	}
	for i, c := range calls {
		n := node.Calls[i]
		denied := []string{}
		for _, d := range n[3].([]any) {
			denied = append(denied, d.(string))
		}
		mine := c.DeniedBy
		if mine == nil {
			mine = []string{}
		}
		if c.Name != n[0] || c.InputJSON() != n[1] || c.Sidechain != n[2] || !reflect.DeepEqual(mine, denied) {
			t.Fatalf("call %d: %+v vs %v", i, c, n)
		}
	}
	if !reflect.DeepEqual(SkillLoads(all), node.Loads) {
		t.Errorf("loads differ")
	}
}

// The loads-only read skips the lines that cannot name a load without
// parsing them; over every shape a load can take, written plainly and
// with its letters escaped, it answers what the full parse answers.
func TestLoadsOnlyReadMatchesTheFullParse(t *testing.T) {
	shapes := []string{
		line(t, assistant(toolUse("a", "Skill", map[string]any{"skill": "plain"}))),
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"escaped-name"}}]}}`,
		line(t, assistant(toolUse("b", "Read", map[string]any{"file_path": "/r/skills/read/SKILL.md"}))),
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"\/r\/skills\/slashes\/SKILL.md"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/r/skills/esc/SKILL.md"}}]}}`,
		line(t, user("<command-name>/cmd</command-name>")),
		`{"type":"user","message":{"content":"<command-name>/lt</command-name>"}}`,
		`{"type":"user","message":{"content":"<command-name>/esc</command-name>"}}`,
		line(t, user([]any{text("<command-name>/blocks</command-name>")})),
		line(t, assistant(toolUse("c", "Bash", map[string]any{"command": "cat skills/x/SKILL.md # Skill command-name"}))),
		line(t, user("nothing here")),
		line(t, toolResult("c", false, "Skill output")),
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"partial"`,
		`not json Skill`,
		``,
	}
	var b strings.Builder
	seed := uint32(7)
	for i := 0; i < 4000; i++ {
		seed = seed*1664525 + 1013904223
		b.WriteString(shapes[int(seed>>16)%len(shapes)])
		b.WriteString("\n")
	}
	data := []byte(b.String())
	full, fast := SkillLoads(Parse(data)), loadsIn(data)
	if !reflect.DeepEqual(full, fast) {
		t.Fatalf("full %d loads, loads-only %d", len(full), len(fast))
	}
	if len(full) < 1000 {
		t.Errorf("only %d loads; the fixture is not exercising the shapes", len(full))
	}
	for _, want := range []string{"plain", "escaped-name", "read", "slashes", "esc", "cmd", "lt", "blocks"} {
		found := false
		for _, l := range full {
			found = found || l == want
		}
		if !found {
			t.Errorf("no %s load among %v...", want, full[:8])
		}
	}
}
