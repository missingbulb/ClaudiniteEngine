package declared

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

func call(t *testing.T, tool, input string) Call {
	t.Helper()
	v, err := jsjson.Decode([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	return Call{Tool: tool, Input: v}
}

func loadOne(t *testing.T, checksJSON string) *Set {
	t.Helper()
	dir := member(t, testSettings, checksJSON, nil)
	s, err := LoadSet(dir, "0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Faults) > 0 {
		t.Fatalf("%+v", s.Faults)
	}
	return s
}

func TestGuardConditions(t *testing.T) {
	g := func(entry string) string {
		return `[{"id":"acme-guard","on_fail":"block","scope":"action","failureMessage":"the why","guardToolCalls":[` + entry + `]}]`
	}
	cases := []struct {
		name, entry string
		call        [2]string
		prior       [][2]string
		want        string
	}{
		{"match with groups", `{"tool":"Bash","inputField":"command","match":"/git (?<verb>push|pull)(?<opt> -f)?/","what":"{tool} {verb}{opt} [{match}] {field}","fix":"no"}`,
			[2]string{"Bash", `{"command":"git pull"}`}, nil, "acme-guard|Bash pullundefined [git pull] git pull|no"},
		{"match misses", `{"tool":"Bash","inputField":"command","match":"/git push/","what":"w","fix":"f"}`, [2]string{"Bash", `{"command":"ls"}`}, nil, ""},
		{"other tool", `{"tool":"Bash","inputField":"command","match":"/x/","what":"w","fix":"f"}`, [2]string{"Read", `{"command":"x"}`}, nil, ""},
		{"regex tool", `{"tool":"/^mcp__gh__/","inputFieldAbsent":["perPage"],"what":"{tool}","fix":"f"}`, [2]string{"mcp__gh__list", `{}`}, nil, "acme-guard|mcp__gh__list|f"},
		{"requireMatch", `{"tool":"Bash","inputField":"command","requireMatch":"/^git /","what":"{field}","fix":"f"}`, [2]string{"Bash", `{"command":"ls"}`}, nil, "acme-guard|ls|f"},
		{"requireMatch holds", `{"tool":"Bash","inputField":"command","requireMatch":"/^git /","what":"{field}","fix":"f"}`, [2]string{"Bash", `{"command":"git x"}`}, nil, ""},
		{"inputMatches keeps order", `{"tool":"Grep","inputMatches":"/\"b\":1,\"a\":\\{\"z\":\"<&>\"\\}/","what":"{match}","fix":"f"}`,
			[2]string{"Grep", `{"b":1,"a":{"z":"<&>"}}`}, nil, `acme-guard|"b":1,"a":{"z":"<&>"}|f`},
		{"field non-string is JSON", `{"tool":"T","inputField":"a.b","match":"/\\[1,2\\]/","what":"{field}","fix":"f"}`, [2]string{"T", `{"a":{"b":[1,2]}}`}, nil, "acme-guard|[1,2]|f"},
		{"inputFieldAbsent present", `{"tool":"Bash","inputFieldAbsent":["description"],"what":"w","fix":"f"}`, [2]string{"Bash", `{"description":null}`}, nil, ""},
		{"atMostPerSession", `{"tool":"Bash","inputField":"command","atMostPerSession":1,"what":"{field}","fix":"f"}`,
			[2]string{"Bash", `{"command":"npm test"}`}, [][2]string{{"Bash", `{"command":"npm test"}`}, {"Read", `{"command":"npm test"}`}}, "acme-guard|npm test|f"},
		{"atMostPerSession under", `{"tool":"Bash","inputField":"command","atMostPerSession":2,"what":"{field}","fix":"f"}`,
			[2]string{"Bash", `{"command":"npm test"}`}, [][2]string{{"Bash", `{"command":"npm test"}`}}, ""},
		{"atMostPerSession whole input", `{"tool":"W","atMostPerSession":1,"what":"w","fix":"f"}`,
			[2]string{"W", `{"b":1,"a":2}`}, [][2]string{{"W", `{"a":2,"b":1}`}}, ""},
		{"unlessMatches", `{"tool":"Bash","inputField":"command","match":"/push/","unlessMatches":"/dry/","what":"w","fix":"f"}`, [2]string{"Bash", `{"command":"push --dry"}`}, nil, ""},
		{"unlessInputMatches", `{"tool":"Bash","inputField":"command","match":"/push/","unlessInputMatches":"/\"x\":true/","what":"w","fix":"f"}`, [2]string{"Bash", `{"command":"push","x":true}`}, nil, ""},
	}
	for _, c := range cases {
		s := loadOne(t, g(c.entry))
		var prior []Call
		for _, p := range c.prior {
			prior = append(prior, call(t, p[0], p[1]))
		}
		hs, err := GuardFindings(s.Checks[0], call(t, c.call[0], c.call[1]), prior)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got := ""
		if len(hs) > 0 {
			got = "acme-guard|" + hs[0].What + "|" + hs[0].Fix
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestGuardVerdict(t *testing.T) {
	checks := `[{"id":"acme-block","on_fail":"block","scope":"action","failureMessage":"blocked why","guardToolCalls":[{"tool":"Bash","inputField":"command","match":"/curl/","what":"curls","fix":"fetch"}]},
	 {"id":"acme-advise","on_fail":"advise","scope":"action","guardToolCalls":[{"tool":"Bash","inputField":"command","match":"/curl/","what":"advised","fix":"f"}]},
	 {"id":"acme-grace","on_fail":"block","since":"2026-01-01","scope":"action","guardToolCalls":[{"tool":"Bash","inputField":"command","match":"/wget/","what":"wgets","fix":"f"}]},
	 {"id":"acme-count","on_fail":"block","scope":"action","guardToolCalls":[{"tool":"Bash","atMostPerSession":1,"what":"again","fix":"f"}]}]`
	s := loadOne(t, checks)
	now := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	noPrior := func() []Call { t.Fatal("prior read for a call no counting guard needs"); return nil }
	keep := func(s *Set, counting bool) {
		var cs []*Check
		for _, c := range s.Checks {
			if (c.ID == "acme-count") == counting {
				cs = append(cs, c)
			}
		}
		s.Checks = cs
	}
	keep(s, false)
	v := s.Guard(call(t, "Bash", `{"command":"curl x"}`), noPrior, now)
	if !reflect.DeepEqual(v.Blocks, []string{"Blocked by acme-block: curls. blocked why. Fix: fetch"}) || len(v.Advice) != 1 {
		t.Errorf("%+v", v)
	}
	v = s.Guard(call(t, "Bash", `{"command":"wget x"}`), noPrior, now)
	if len(v.Blocks) != 0 || !reflect.DeepEqual(v.Advice, []string{"[claudinite acme-grace] wgets. Fix: f"}) {
		t.Errorf("grace: %+v", v)
	}
	s.Config.Rules = map[string]string{"acme-block": "advise", "acme-advise": "off"}
	v = s.Guard(call(t, "Bash", `{"command":"curl x"}`), noPrior, now)
	if len(v.Blocks) != 0 || !reflect.DeepEqual(v.Advice, []string{"[claudinite acme-block] curls. blocked why. Fix: fetch"}) {
		t.Errorf("override: %+v", v)
	}
	s.Config.Rules = map[string]string{}
	v = s.Guard(call(t, "Bash", `{"command":"git push origin --delete x"}`), noPrior, now)
	if len(v.Blocks) != 1 || !strings.HasPrefix(v.Blocks[0], "Blocked: never delete a remote branch") {
		t.Errorf("remote-branch-delete: %+v", v)
	}
	for _, cmd := range []string{"git push origin :x", "git push -d origin x"} {
		if v := s.Guard(call(t, "Bash", `{"command":"`+cmd+`"}`), noPrior, now); len(v.Blocks) != 1 {
			t.Errorf("%s: %+v", cmd, v)
		}
	}
	if v := s.Guard(call(t, "Bash", `{"command":"git push origin x"}`), noPrior, now); len(v.Blocks) != 0 {
		t.Errorf("a push is no delete: %+v", v)
	}
	s.Config.Rules = map[string]string{BuiltinRemoteBranchDelete: "off"}
	if v := s.Guard(call(t, "Bash", `{"command":"git push origin --delete x"}`), noPrior, now); len(v.Blocks) != 0 {
		t.Errorf("off: %+v", v)
	}
	// A repo that is no member still never deletes a remote branch.
	none := &Set{Config: Config{Rules: map[string]string{}}}
	if v := none.Guard(call(t, "Bash", `{"command":"git push origin --delete x"}`), noPrior, now); len(v.Blocks) != 1 {
		t.Errorf("no member: %+v", v)
	}
	s = loadOne(t, checks)
	keep(s, true)
	reads := 0
	prior := func() []Call { reads++; return []Call{call(t, "Bash", `{"command":"ls"}`)} }
	if v := s.Guard(call(t, "Bash", `{"command":"ls"}`), prior, now); len(v.Blocks) != 1 || reads != 1 {
		t.Errorf("count: %+v %d", v, reads)
	}
}

func TestAGuardThatFailsLetsTheCallThrough(t *testing.T) {
	old := MatchTimeout
	MatchTimeout = 20 * time.Millisecond
	t.Cleanup(func() { MatchTimeout = old })
	s := loadOne(t, `[{"id":"acme-slow","on_fail":"block","scope":"action","guardToolCalls":[{"tool":"Bash","inputField":"command","match":"/^(a+)+$/","what":"w","fix":"f"}]},
	  {"id":"acme-nowhat","on_fail":"block","scope":"action","guardToolCalls":[{"tool":"Bash","inputField":"command","match":"/a/"}]}]`)
	v := s.Guard(call(t, "Bash", `{"command":"`+strings.Repeat("a", 40)+`b"}`), func() []Call { return nil }, time.Now())
	if len(v.Blocks) != 0 || len(v.Errors) != 2 || !strings.Contains(v.Errors[0], "acme-pack/acme-") {
		t.Errorf("%+v", v)
	}
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func jl(t *testing.T, v any) string {
	t.Helper()
	b, _ := json.Marshal(v)
	return string(b)
}

func useEntry(t *testing.T, id, tool string, input any, sidechain bool) string {
	return jl(t, map[string]any{"type": "assistant", "isSidechain": sidechain, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": id, "name": tool, "input": input}}}})
}

func resultEntry(t *testing.T, id, text string) string {
	return jl(t, map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": true, "content": text}}}})
}

func TestActionBackstopAtStop(t *testing.T) {
	checks := `[{"id":"acme-guard","on_fail":"block","scope":"action","guardToolCalls":[{"tool":"Bash","inputField":"command","match":"/push/","what":"pushes","fix":"f"}]}]`
	dir := member(t, testSettings, checks, nil)
	p := writeTranscript(t,
		useEntry(t, "a", "Bash", map[string]any{"command": "git push"}, false),
		resultEntry(t, "a", "Blocked by acme-guard: pushes"),
		useEntry(t, "b", "Read", map[string]any{"file_path": "x"}, false),
		useEntry(t, "c", "Bash", map[string]any{"command": "git push again"}, true))
	fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}, Session: transcript.NewSession(p)}, time.Now())
	var got []string
	for _, f := range fs {
		got = append(got, string(f.Class)+" "+f.Path+" "+f.Sentence)
	}
	want := []string{"advisory (session) Bash call #1 pushes (denied at the hook)", "advisory (session) Bash call #2 pushes"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	fs, stderr := runSet(t, dir, Selection{Tags: []string{"work"}}, time.Now())
	if len(fs) != 0 || !strings.Contains(stderr, "assert nothing without one") {
		t.Errorf("no transcript: %+v %q", fs, stderr)
	}
	if fs, _ := runSet(t, dir, Selection{Tags: []string{"world"}, Session: transcript.NewSession(p)}, time.Now()); len(fs) != 0 {
		t.Errorf("an action check is no world check: %+v", fs)
	}
	s, _ := LoadSet(dir, "0.0.0")
	for _, l := range s.List() {
		if l.ID == "acme-guard" && !reflect.DeepEqual(l.Tags, []string{"action", "work", "pre-tool-use", "declared", "acme-pack"}) {
			t.Errorf("tags %v", l.Tags)
		}
	}
}

func TestReplyClassGate(t *testing.T) {
	check := `[{"id":"acme-gated","on_fail":"block","scope":"work","failureMessage":"w","fix":"f",
	  "whenReplyClassIncludes":["feature","process-change"],"flagUntrackedFilesMatching":[{"match":"/^U$/","what":"x"}]}]`
	dir := member(t, testSettings, check, nil)
	put(t, dir, map[string]string{"U": "x\n"})
	turn := func(class string) string {
		return writeTranscript(t, jl(t, map[string]any{"type": "user", "message": map[string]any{"content": "do it"}}),
			jl(t, map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Comment class: " + class}}}}))
	}
	for class, want := range map[string]int{"process change": 1, "feature": 1, "other": 0} {
		if fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}, Session: transcript.NewSession(turn(class))}, time.Now()); len(fs) != want {
			t.Errorf("%s: %+v", class, fs)
		}
	}
	if fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}}, time.Now()); len(fs) != 0 {
		t.Errorf("no transcript: %+v", fs)
	}
}

func TestSkillLoadedBeforeEditing(t *testing.T) {
	skill := "---\nname: guide\nmetadata:\n  force-load-on-file-edits-paths: ['docs/**']\n  force-load-on-tool-calls: ['Bash.command /deploy/']\n---\n"
	dir := member(t, testSettings, `[]`, map[string]string{
		".claudinite/local/packs/acme-pack/skills/guide/SKILL.md": skill, "docs/a.md": "a\n", "src/b.md": "b\n"})
	gitIn(t, dir, "checkout", "-q", "-b", "change")
	put(t, dir, map[string]string{"docs/a.md": "edited\n", "src/b.md": "edited\n"})
	gitIn(t, dir, "commit", "-qam", "change")
	p := writeTranscript(t, useEntry(t, "a", "Bash", map[string]any{"command": "make deploy"}, false), useEntry(t, "b", "Bash", map[string]any{"command": "deploy again"}, false))
	fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}, Session: transcript.NewSession(p)}, time.Now())
	var got []string
	for _, f := range fs {
		got = append(got, f.Name()+" "+f.Path+" "+string(f.Class))
	}
	want := []string{"cn/skill-loaded-before-editing docs/a.md finding", "cn/skill-loaded-before-editing (session) Bash call finding"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(fs[0].Sentence, "changed under docs/**, which the acme-pack pack's `guide` skill forces itself for") {
		t.Error(fs[0].Sentence)
	}
	if fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}, Session: transcript.NewSession(p), SkipForcedLoading: true}, time.Now()); len(fs) != 0 {
		t.Errorf("license off: %+v", fs)
	}
	loaded := writeTranscript(t, useEntry(t, "a", "Skill", map[string]any{"skill": "guide"}, true), useEntry(t, "b", "Bash", map[string]any{"command": "make deploy"}, false))
	if fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}, Session: transcript.NewSession(loaded)}, time.Now()); len(fs) != 0 {
		t.Errorf("loaded: %+v", fs)
	}
	if fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}}, time.Now()); len(fs) != 0 {
		t.Errorf("no transcript: %+v", fs)
	}
	var _ = findings.Coded
}
