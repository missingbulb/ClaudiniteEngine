package scaffold

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
)

func repo(t *testing.T, settings string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claudinite"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claudinite", "settings.yaml"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

var now = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func TestANewLocalPackLoadsDeclaredAndChecksClean(t *testing.T) {
	root := repo(t, "engine:\n  version: \"1.1.0\"\n")
	made, err := New(Request{Repo: root, Name: "acme-pack", Belongs: "acme's conventions", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".claudinite/local/packs/acme-pack/pack.json", ".claudinite/local/packs/acme-pack/RULES.md", ".claudinite/local/packs/acme-pack/provenance/_pack.md"}
	if strings.Join(made.Files, " ") != strings.Join(want, " ") || made.Settings != ".claudinite/settings.yaml" || made.Token != "local/acme-pack" {
		t.Fatalf("%+v", made)
	}
	s, err := packset.Load(root, "0.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Packs) != 1 || len(s.NotLoaded) != 0 || len(s.Declared.Local) != 1 || s.Declared.Local[0] != "acme-pack" {
		t.Fatalf("loaded %+v not loaded %+v", s.Packs, s.NotLoaded)
	}
	var out, errb bytes.Buffer
	if code := provenance.Main([]string{"check", "local/acme-pack"}, root, nil, &out, &errb); code != 0 {
		t.Fatalf("check exit %d:\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "_pack.md ← the manifest\n") {
		t.Fatalf("check said:\n%s", out.String())
	}
}

func TestNewRefusesADeclaredOrPresentName(t *testing.T) {
	for name, c := range map[string]struct{ settings, pre string }{
		"declared local": {settings: "packs:\n  declared:\n    - local/acme-pack\n"},
		"declared canon": {settings: "packs:\n  declared:\n    - acme-pack\n"},
		"present":        {settings: "engine:\n  version: \"1.1.0\"\n", pre: ".claudinite/local/packs/acme-pack/RULES.md"},
	} {
		root := repo(t, c.settings)
		if c.pre != "" {
			_ = os.MkdirAll(filepath.Dir(filepath.Join(root, c.pre)), 0o755)
			_ = os.WriteFile(filepath.Join(root, c.pre), []byte("x\n"), 0o644)
		}
		before, _ := os.ReadFile(filepath.Join(root, ".claudinite", "settings.yaml"))
		if _, err := New(Request{Repo: root, Name: "acme-pack", Now: now}); err == nil {
			t.Errorf("%s: no refusal", name)
		}
		if after, _ := os.ReadFile(filepath.Join(root, ".claudinite", "settings.yaml")); !bytes.Equal(before, after) {
			t.Errorf("%s: the settings changed", name)
		}
	}
	if _, err := New(Request{Repo: repo(t, "engine: {}\n"), Name: "Acme", Now: now}); err == nil {
		t.Error("a malformed name")
	}
}
