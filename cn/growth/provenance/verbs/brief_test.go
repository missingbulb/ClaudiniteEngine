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
