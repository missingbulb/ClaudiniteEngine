package sdkserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

type fakeTree struct{ root string }

func (fakeTree) Files() []string         { return []string{"a.md", "b.yaml"} }
func (fakeTree) TrackedList() []string   { return []string{"a.md", "b.yaml", "v.md"} }
func (fakeTree) AllFiles() []string      { return []string{"a.md", "b.yaml", "v.md"} }
func (fakeTree) UntrackedList() []string { return nil }
func (fakeTree) ChangedFiles() []string  { return []string{"a.md"} }
func (fakeTree) Deleted() []string       { return []string{"old.md"} }
func (fakeTree) Branch() string          { return "change" }
func (fakeTree) BaseRefName() string     { return "origin/main" }
func (fakeTree) MergeBase() string       { return "abc" }
func (fakeTree) ReadBase(p string) (string, bool) {
	if p == "a.md" {
		return "base text", true
	}
	return "", false
}
func (fakeTree) ListBase() []string { return []string{"a.md", "old.md"} }
func (fakeTree) AddedLines(f string) []gitcmd.Line {
	return []gitcmd.Line{{Line: 1, Text: "added in " + f}}
}
func (fakeTree) RemovedLines(string) []gitcmd.Line { return nil }
func (fakeTree) CommitsWithFiles() []gitcmd.Commit {
	return []gitcmd.Commit{{Sha: "s1", Date: "2026-10-01T00:00:00Z", Subject: "one", Files: []string{"a.md"}}}
}
func (fakeTree) Commits() []string                { return []string{"one\nbody"} }
func (fakeTree) IntroducedMerges() []gitcmd.Merge { return nil }
func (fakeTree) GrepTracked(n string) []gitcmd.Hit {
	return []gitcmd.Hit{{Path: "v.md", Line: 2, Text: "x " + n}}
}

func session(t *testing.T) *transcript.Session {
	t.Helper()
	dir := t.TempDir()
	lines := []string{
		`{"type":"user","timestamp":"2026-10-01T10:00:00Z","message":{"role":"user","content":"please add it"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Comment class: feature\nOn it."},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
	}
	p := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return transcript.NewSession(p)
}

func ask(t *testing.T, s *Server, method, args string) string {
	t.Helper()
	res, err := s.Handle(method, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return string(res)
}

func TestEveryMethodAnswersJSON(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "b.yaml"), []byte("Resources:\n  F:\n    Handler: !Ref Name\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "p.toml"), []byte("[project]\nname = \"x\"\n"), 0o644)
	cfg := Config{PackConfig: map[string]map[string]any{"acme-pack": {"probe": true}}, Rules: map[string]string{"acme-check": "off"},
		Packs: []packset.Pack{
			{ID: "acme-pack", Kind: packset.Canon, Dir: filepath.Join(root, ".claudinite/shared/packs/acme-pack"), Rel: ".claudinite/shared/packs/acme-pack", Version: "1.2", MinEngine: "61001.1.0", Prose: "RULES.md", Skills: []string{"how"}, Requires: []string{"basics"}},
			{ID: "mine", Kind: packset.Local, Rel: ".claudinite/local/packs/mine"},
		}}
	s := Serve(root, fakeTree{root}, session(t), cfg)
	cases := map[string][2]string{
		"tree.files":           {`{}`, `["a.md","b.yaml"]`},
		"tree.tracked":         {`{}`, `["a.md","b.yaml","v.md"]`},
		"tree.untracked":       {`{}`, `[]`},
		"change.files":         {`{}`, `["a.md"]`},
		"change.deleted":       {`{}`, `["old.md"]`},
		"change.base":          {`{}`, `{"branch":"change","baseRef":"origin/main","mergeBase":"abc"}`},
		"change.readBase":      {`{"path":"a.md"}`, `{"text":"base text","ok":true}`},
		"change.listBase":      {`{}`, `["a.md","old.md"]`},
		"change.added":         {`{"files":null}`, `[{"path":"a.md","line":1,"text":"added in a.md"}]`},
		"change.removed":       {`{"files":["a.md"]}`, `[]`},
		"change.commits":       {`{}`, `[{"sha":"s1","date":"2026-10-01T00:00:00Z","subject":"one","files":["a.md"]}]`},
		"change.messages":      {`{}`, `["one\nbody"]`},
		"change.merges":        {`{}`, `[]`},
		"change.grep":          {`{"needle":"old.md"}`, `[{"path":"v.md","line":2,"text":"x old.md"}]`},
		"session.replyClasses": {`{}`, `["feature"]`},
		"session.toolCalls":    {`{}`, `[{"tool":"Bash","input":{"command":"ls"},"sidechain":false,"deniedBy":[]}]`},
		"session.skillLoads":   {`{}`, `[]`},
		"config.pack":          {`{"id":"acme-pack"}`, `{"probe":true}`},
		"config.checks":        {`{}`, `{"rules":{"acme-check":"off"},"accept":[]}`},
		"doc.parse":            {`{"path":"b.yaml"}`, `{"Resources":{"F":{"Handler":"Name"}}}`},
		"packs.list": {`{}`, `[{"id":"acme-pack","kind":"canon","dir":".claudinite/shared/packs/acme-pack","version":"1.2","minEngineVersion":"61001.1.0","prose":"RULES.md","skills":["how"],"requires":["basics"]},` +
			`{"id":"mine","kind":"local","dir":".claudinite/local/packs/mine","version":"","minEngineVersion":"","prose":"","skills":[],"requires":[]}]`},
	}
	for m, c := range cases {
		if got := ask(t, s, m, c[0]); got != c[1] {
			t.Errorf("%s %s = %s, want %s", m, c[0], got, c[1])
		}
	}
	if got := ask(t, s, "config.pack", `{"id":"other"}`); got != "null" {
		t.Errorf("config.pack of a pack with no config: %s", got)
	}
	if got := ask(t, s, "doc.parse", `{"path":"p.toml"}`); got != `{"project":{"name":"x"}}` {
		t.Errorf("toml: %s", got)
	}
	turns := ask(t, s, "session.ownerTurns", `{}`)
	if !strings.Contains(turns, `"text":"please add it"`) || !strings.Contains(turns, `"classes":["feature"]`) || !strings.Contains(turns, `"classLine":"Comment class: feature"`) || !strings.Contains(turns, `"timestamp":"2026-10-01T10:00:00Z"`) {
		t.Errorf("ownerTurns %s", turns)
	}
	if len(s.Methods()) != len(cases)+2 {
		t.Errorf("methods %v", s.Methods())
	}
}

func TestRefusals(t *testing.T) {
	s := Serve(t.TempDir(), fakeTree{}, nil, Config{})
	for _, c := range [][2]string{{"doc.parse", `{"path":"../x.json"}`}, {"doc.parse", `{"path":"/etc/passwd"}`}, {"change.readBase", `{"path":"../x"}`}, {"doc.parse", `{"path":"missing.json"}`}, {"dance", `{}`}, {"doc.parse", `{"path":"a.txt"}`}} {
		if _, err := s.Handle(c[0], json.RawMessage(c[1])); err == nil {
			t.Errorf("%s %s answered", c[0], c[1])
		}
	}
	// A run with no session answers empty lists, never an error.
	for _, m := range []string{"session.ownerTurns", "session.replyClasses", "session.toolCalls", "session.skillLoads"} {
		if got := ask(t, s, m, `{}`); got != "[]" {
			t.Errorf("%s with no session: %s", m, got)
		}
	}
}
