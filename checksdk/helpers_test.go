package checksdk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The comment stripper's cases are the Node helper's own
// (engine-tests/source.test.mjs at Claudinite@057841ac).
func TestStripComments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"const x = 1; // set x\n", "const x = 1; \n"},
		{"a\n/* two\nlines */\nb", "a\n\n\nb"},
		{"const url = 'https://example.com/a'; // note\n", "const url = 'https://example.com/a'; \n"},
		{`const s = "a\"// b"; // c` + "\n", `const s = "a\"// b"; ` + "\n"},
		{"const t = `x /* y */ z`;\n", "const t = `x /* y */ z`;\n"},
		{"import { a } from './page-adapter/dom.js';\n", "import { a } from './page-adapter/dom.js';\n"},
		{`const re = /instructions" must be a string/; // note` + "\n", `const re = /instructions" must be a string/; ` + "\n"},
		{"const re = /the pack's id/; // note\nconst x = 1; // gone\n", "const re = /the pack's id/; \nconst x = 1; \n"},
		{`const re = /https:\/\/x/; // note` + "\n", `const re = /https:\/\/x/; ` + "\n"},
		{`const re = /[/"]a/; // note` + "\n", `const re = /[/"]a/; ` + "\n"},
		{"const q = total / count; // per item\nconst s = \"kept\";\n", "const q = total / count; \nconst s = \"kept\";\n"},
		{"const t = `a${xs.map((x) => `- \\`${x}\\``).join(\"\")}b`;\n// note\nconst s = \"kept\";\n",
			"const t = `a${xs.map((x) => `- \\`${x}\\``).join(\"\")}b`;\n\nconst s = \"kept\";\n"},
		{"const t = `a${/* drop */ x}b`;\n", "const t = `a${ x}b`;\n"},
	}
	for _, c := range cases {
		if got := StripComments(c.in); got != c.want {
			t.Errorf("StripComments(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestBlankFences(t *testing.T) {
	got := BlankFences("a\n```js\nx\n```\nb\n  ~~~\ny\n~~~\n")
	if got != "a\n\n\n\nb\n\n\n\n" {
		t.Errorf("got %q", got)
	}
}

func TestCommentOnly(t *testing.T) {
	cases := []struct {
		file       string
		before     *string
		after      *string
		want       bool
		reasonCase string
	}{
		{"a.js", str("const x = 1; // one\n"), str("const x = 1; // two\n\n"), true, "a comment and a blank line"},
		{"a.js", str("const x = 1;\n"), str("const y = 1;\n"), false, "a rename"},
		{"a.py", str("x = 1 # one\n"), str("x = 1 # two\n"), false, "a language it cannot read"},
		{"a.js", nil, str("x\n"), false, "an added file"},
		{"a.js", str("x\n"), nil, false, "a deleted file"},
		{"a.GO", str("x := 1 // a\n"), str("  x := 1 /* b */\n"), true, "indentation and an upper-case extension"},
		{"Makefile", str("a"), str("a"), false, "no extension"},
	}
	for _, c := range cases {
		if got := CommentOnly(c.file, c.before, c.after); got != c.want {
			t.Errorf("%s: CommentOnly = %v", c.reasonCase, got)
		}
	}
	if !CommentCheckable[".mjs"] || CommentCheckable[".py"] {
		t.Error("CommentCheckable")
	}
}

func str(s string) *string { return &s }

// The link extractor's cases are the Node helper's own
// (engine-tests/markdown.test.mjs).
func TestExtractLinks(t *testing.T) {
	if got := ExtractLinks("intro\n\nsee [the doc](sub/doc.md) here\n"); !reflect.DeepEqual(got, []Link{{Line: 3, Target: "sub/doc.md", Label: "the doc"}}) {
		t.Errorf("relative: %v", got)
	}
	if got := ExtractLinks("[`a/b.md`](a/b.md#section)\n"); len(got) != 1 || got[0].Label != "a/b.md" || got[0].Target != "a/b.md" {
		t.Errorf("anchor and backticks: %v", got)
	}
	if got := ExtractLinks("[x](https://e.com/a.md) [y](mailto:a@b.c) [z](#local)\n"); len(got) != 0 {
		t.Errorf("external: %v", got)
	}
	if got := ExtractLinks("before\n```\n[gone](missing.md)\n```\n[kept](real.md)\n"); len(got) != 1 || got[0].Target != "real.md" {
		t.Errorf("fence: %v", got)
	}
	if got := ExtractLinks("![shot](img/a.png)\n"); len(got) != 1 || got[0].Target != "img/a.png" {
		t.Errorf("image: %v", got)
	}
	if got := ExtractLinks("see `[x](y.md)` and [z](w.md \"title\")\n"); len(got) != 1 || got[0].Target != "w.md" {
		t.Errorf("code span and title: %v", got)
	}
}

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens(4200) != 1000 || EstimateTokens(0) != 0 || EstimateTokens(428) != 102 {
		t.Error("EstimateTokens")
	}
	// x.5 rounds up, as Math.round does.
	if EstimateTokens(21) != 5 || EstimateTokens(23) != 5 {
		t.Errorf("rounding: %d %d", EstimateTokens(21), EstimateTokens(23))
	}
	if CountChars("a😀") != 3 {
		t.Errorf("CountChars counts UTF-16 units, as String.length does: %d", CountChars("a😀"))
	}
}

func TestRepoHelpersOverAFakeEngine(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "[ok](docs/a.md) [dead](docs/gone.md) [out](../x.md)\n")
	write("docs/a.md", "see old/thing.md and lib/old/thing.md\n")
	write("lib/old/thing.md", "kept\n")
	write(".github/workflows/ci.yml", "on: push\n")
	write("package.json", `{"dependencies":{"a":"1","b":"2"}}`)
	f := &Fake{
		Deleted: []string{"old/thing.md"},
		Base:    map[string]string{"package.json": `{"dependencies":{"a":"1"}}`},
	}
	repo := f.Repo(root)
	dead := DeadLinks(repo, nil)
	if len(dead) != 1 || dead[0].Target != "docs/gone.md" || dead[0].Path != "README.md" || dead[0].Line != 1 {
		t.Errorf("DeadLinks: %v", dead)
	}
	if refs := DanglingReferences(repo, nil); len(refs) != 1 || refs[0].Path != "docs/a.md" || refs[0].Gone != "old/thing.md" {
		t.Errorf("DanglingReferences: %v", refs)
	}
	if refs := DanglingReferences(repo, func(string) bool { return true }); len(refs) != 0 {
		t.Errorf("tolerated: %v", refs)
	}
	if wf := WorkflowFiles(repo); !reflect.DeepEqual(wf, []string{".github/workflows/ci.yml"}) {
		t.Errorf("WorkflowFiles: %v", wf)
	}
	head, base := JSONPair(repo, "package.json")
	if len(head.(map[string]any)["dependencies"].(map[string]any)) != 2 || len(base.(map[string]any)["dependencies"].(map[string]any)) != 1 {
		t.Errorf("JSONPair: %v %v", head, base)
	}
	if h, b := JSONPair(repo, "absent.json"); h != nil || b != nil {
		t.Errorf("JSONPair of nothing: %v %v", h, b)
	}
	if got := FilesContaining(repo, "kept", nil); !reflect.DeepEqual(got, []string{"lib/old/thing.md"}) {
		t.Errorf("FilesContaining: %v", got)
	}
	lines := MatchingLines(repo, []string{"docs/a.md", "README.md"}, mustRe(`old/`))
	if len(lines) != 1 || lines[0].Path != "docs/a.md" || lines[0].Line != 1 {
		t.Errorf("MatchingLines: %v", lines)
	}
}

// Packs is the declared pack set, one engine call for the run however
// often a check asks; the Fake answers its own Packs field.
func TestPacksIsOneCallForTheRun(t *testing.T) {
	calls := 0
	repo := NewRepo(t.TempDir(), func(method string, _ json.RawMessage) (any, error) {
		if method != "packs.list" {
			t.Fatalf("unexpected call %s", method)
		}
		calls++
		return json.RawMessage(`[{"id":"acme-pack","kind":"canon","dir":".claudinite/shared/packs/acme-pack","version":"1.2","minEngineVersion":"61001.1.0","prose":"RULES.md","skills":["how"],"requires":["basics"]}]`), nil
	})
	want := []Pack{{ID: "acme-pack", Kind: "canon", Dir: ".claudinite/shared/packs/acme-pack", Version: "1.2", MinEngineVersion: "61001.1.0", Prose: "RULES.md", Skills: []string{"how"}, Requires: []string{"basics"}}}
	for range 2 {
		if got := repo.Packs(); !reflect.DeepEqual(got, want) {
			t.Errorf("Packs %+v", got)
		}
	}
	if calls != 1 {
		t.Errorf("%d calls, want 1", calls)
	}
	fake := (&Fake{Packs: []Pack{{ID: "mine", Kind: "local"}}}).Repo(t.TempDir())
	if got := fake.Packs(); len(got) != 1 || got[0].ID != "mine" {
		t.Errorf("Fake Packs %+v", got)
	}
	if got := (&Fake{}).Repo(t.TempDir()).Packs(); got == nil || len(got) != 0 {
		t.Errorf("an empty Fake answers no packs, not nil: %#v", got)
	}
}
