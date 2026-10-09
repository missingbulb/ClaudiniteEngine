package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/rules/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/hooks"
)

// cn adopt with no pack writes the index; the retired cn rules-index still
// does, saying what it ran instead.
func TestAdoptWritesTheRulesIndex(t *testing.T) {
	t.Parallel()
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
	if out, _, code := runCN(t, bin, nil, "", "adopt", "--repo", repo); code != 0 || !strings.Contains(out, "wrote") {
		t.Errorf("write: exit %d %s", code, out)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/cache/claudinite-rules.GENERATED.md"))
	if string(raw) != "@../shared/packs/hello/RULES.md\n" {
		t.Errorf("%q", raw)
	}
	if out, _, code := runCN(t, bin, nil, "", "adopt", "--repo", repo); code != 0 || !strings.Contains(out, "already current") {
		t.Errorf("current: exit %d %s", code, out)
	}

	// A member holding the index under the legacy directory: the command
	// moves it and says which path went and which came.
	if err := os.Rename(filepath.Join(repo, ".claudinite/cache"), filepath.Join(repo, ".claudinite/flat")); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCN(t, bin, nil, "", "rules-index", "--repo", repo)
	if !strings.Contains(errOut, "`rules-index` is retired; ran `cn adopt --repo") {
		t.Errorf("no retirement notice: %s", errOut)
	}
	if code != 0 || !strings.Contains(out, "removed .claudinite/flat/claudinite-rules.GENERATED.md\nwrote .claudinite/cache/claudinite-rules.GENERATED.md\n") {
		t.Errorf("move: exit %d %s", code, out)
	}
}

// hooks cannot import rulesindex, so its line spells the index and the
// import itself; this keeps both spellings equal to the writer's.
func TestTheMissingImportLineNamesTheWritersIndex(t *testing.T) {
	t.Parallel()
	if !strings.Contains(hooks.MissingImport, " import "+rulesindex.File+";") || !strings.Contains(hooks.MissingImport, `"`+rulesindex.Import+`"`) {
		t.Errorf("hooks.MissingImport %q drifted from %s / %s", hooks.MissingImport, rulesindex.File, rulesindex.Import)
	}
}

// SessionStart's writer refreshes a legacy member's files where they are
// and moves nothing: no CLAUDE.md edit lands in the session's tree, and
// the old import still counts as the import.
func TestTheHookWriterLeavesALegacyMemberInPlace(t *testing.T) {
	repo := t.TempDir()
	for rel, body := range map[string]string{
		".claudinite/settings.yaml":                      "packs:\n  declared:\n    - hello\n",
		".claudinite/shared/packs/hello/pack.json":       `{"version": "1.0", "minEngineVersion": "0.0.0"}`,
		".claudinite/shared/packs/hello/RULES.md":        "- hi\n",
		".claudinite/flat/claudinite-rules.GENERATED.md": "@../shared/packs/stale/RULES.md\n",
		"CLAUDE.md": "# mine\n" + rulesindex.LegacyImport + "\n",
	} {
		p := filepath.Join(repo, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !(hookIndex{}).HasImport(repo) {
		t.Error("the legacy import does not count while the index is under the legacy directory")
	}
	if _, err := (hookIndex{}).Write(repo, "0.0.0"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md")); string(raw) != "# mine\n"+rulesindex.LegacyImport+"\n" {
		t.Errorf("the hook edited CLAUDE.md: %q", raw)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claudinite/cache")); err == nil {
		t.Error("the hook created .claudinite/cache")
	}
	if raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/flat/claudinite-rules.GENERATED.md")); string(raw) != "@../shared/packs/hello/RULES.md\n" {
		t.Errorf("the legacy index was not refreshed in place: %q", raw)
	}
}
