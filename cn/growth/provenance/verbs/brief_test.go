package verbs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	prov "github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance"
)

func TestADeclarationIsComparedByValueNotSpelling(t *testing.T) {
	escaped, ok := declarationOf(`[{"id": "x", "fix": "take it — now", "n": 1.0, "p": "a\/b"}]`, "x")
	if !ok {
		t.Fatal("escaped declaration not found")
	}
	literal, ok := declarationOf(`[{"id":"x","fix":"take it — now","n":1,"p":"a/b"}]`, "x")
	if !ok {
		t.Fatal("literal declaration not found")
	}
	if escaped != literal {
		t.Errorf("one value, two readings:\n%s\n%s", escaped, literal)
	}
	other, _ := declarationOf(`[{"id":"x","fix":"take it — later","n":1,"p":"a/b"}]`, "x")
	if other == literal {
		t.Error("a changed value reads as unchanged")
	}
}

func TestADeclarationIsFoundByItsExactID(t *testing.T) {
	if _, ok := declarationOf(`[{"ID":"x"},{"id":"y"}]`, "x"); ok {
		t.Error(`"ID" read as "id"`)
	}
	if _, ok := declarationOf(`{"id":"x"}`, "x"); ok {
		t.Error("a file that is not an array declared a check")
	}
}

// A check the engine carries has no history in the pack, and the brief
// says the engine carries it rather than asking whether the clone is
// shallow.
func TestTheBriefNamesAnEngineCarriedCheck(t *testing.T) {
	prov.RegisterEngineCheck("acme-engine", "acme-built-in")
	root := t.TempDir()
	for rel, text := range map[string]string{
		"packs/acme-engine/pack.json":                   "{\"version\": \"1\"}\n",
		"packs/acme-engine/RULES.md":                    "# acme\n",
		"packs/acme-engine/provenance/acme-built-in.md": "",
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=a", "-c", "user.email=a@b", "add", "-A"},
		{"-c", "user.name=a", "-c", "user.email=a@b", "commit", "-qm", "acme: the pack (#1)"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	code, stdout, stderr := run(root, "", "backfill", "acme-engine")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if strings.Contains(stdout, "shallow") || !strings.Contains(stdout, "## carried by the engine") || !strings.Contains(stdout, "acme-built-in") {
		t.Errorf("brief:\n%s", stdout)
	}
}

// A rule reworded in place is briefed with the commit that reworded it,
// read from each commit's copy of RULES.md.
func TestTheBriefNamesTheCommitThatRewordedARule(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=a", "-c", "user.email=a@b"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write("packs/acme-pack/pack.json", "{\"version\": \"1\"}\n")
	write("packs/acme-pack/RULES.md", "# acme\n\n- **Doing a thing** — do it once. (doing-thing)\n")
	write("packs/acme-pack/provenance/doing-thing.md", "")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "acme: the rule (#1)")
	write("packs/acme-pack/RULES.md", "# acme\n\n- **Doing a thing** — do it once, and say so. (doing-thing)\n")
	git("commit", "-qam", "acme: reword the rule (#2)")
	write("packs/acme-pack/pack.json", "{\"version\": \"2\"}\n")
	git("commit", "-qam", "acme: a version (#3)")
	code, stdout, stderr := run(root, "", "backfill", "acme-pack", "doing-thing")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "reword the rule · RULES.md · doing-thing (reworded)") {
		t.Errorf("the brief does not name #2 as the rewording:\n%s", stdout)
	}
}
