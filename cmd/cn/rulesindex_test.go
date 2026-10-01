package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/hooks"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/rulesindex"
)

func TestRulesIndexCommand(t *testing.T) {
	bin := buildCN(t, "")
	repo := t.TempDir()
	for rel, body := range map[string]string{
		".claudinite/settings.yaml":                "packs:\n  declared:\n    - hello\n",
		".claudinite/shared/packs/hello/pack.json": `{"version": "1.0", "minEngineVersion": "0.0.0"}`,
		".claudinite/shared/packs/hello/RULES.md":  "- hi\n",
	} {
		p := filepath.Join(repo, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, _, code := runCN(t, bin, nil, "", "rules-index", "--check", "--repo", repo); code != 1 || !strings.Contains(out, "absent") {
		t.Errorf("absent: exit %d %s", code, out)
	}
	if out, _, code := runCN(t, bin, nil, "", "rules-index", "--repo", repo); code != 0 || !strings.Contains(out, "wrote") {
		t.Errorf("write: exit %d %s", code, out)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/flat/claudinite-rules.GENERATED.md"))
	if string(raw) != "@../shared/packs/hello/RULES.md\n" {
		t.Errorf("%q", raw)
	}
	if out, _, code := runCN(t, bin, nil, "", "rules-index", "--check", "--repo", repo); code != 0 || !strings.Contains(out, "current") {
		t.Errorf("current: exit %d %s", code, out)
	}
}

// hooks cannot import rulesindex, so its line spells the index and the
// import itself; this keeps both spellings equal to the writer's.
func TestTheMissingImportLineNamesTheWritersIndex(t *testing.T) {
	if !strings.Contains(hooks.MissingImport, " import "+rulesindex.File+";") || !strings.Contains(hooks.MissingImport, `"`+rulesindex.Import+`"`) {
		t.Errorf("hooks.MissingImport %q drifted from %s / %s", hooks.MissingImport, rulesindex.File, rulesindex.Import)
	}
}
