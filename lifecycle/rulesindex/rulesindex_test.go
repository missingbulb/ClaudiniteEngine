package rulesindex

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func member(t *testing.T) string {
	repo := t.TempDir()
	write(t, filepath.Join(repo, ".claudinite/settings.yaml"), "packs:\n  declared:\n    - web\n    - basics\n    - local/mine\n    - quiet\n")
	write(t, filepath.Join(repo, ".claudinite/shared/packs/basics/pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(repo, ".claudinite/shared/packs/basics/RULES.md"), "- b\n")
	write(t, filepath.Join(repo, ".claudinite/shared/packs/web/pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(repo, ".claudinite/shared/packs/web/RULES.md"), "- w\n")
	write(t, filepath.Join(repo, ".claudinite/shared/packs/quiet/pack.json"), `{"version": "1.0", "prose": null}`)
	write(t, filepath.Join(repo, ".claudinite/shared/packs/quiet/RULES.md"), "- doc\n")
	write(t, filepath.Join(repo, ".claudinite/local/packs/mine/pack.json"), `{}`)
	write(t, filepath.Join(repo, ".claudinite/local/packs/mine/RULES.md"), "- m\n")
	return repo
}

// The Node engine's bytes: canon by name, then local, POSIX paths relative
// to the index, the person's literal line last when a pack copies one.
func TestContentMatchesNode(t *testing.T) {
	repo := member(t)
	got, err := Content(repo, "0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	want := "@../shared/packs/basics/RULES.md\n@../shared/packs/web/RULES.md\n@../local/packs/mine/RULES.md\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	write(t, filepath.Join(repo, ".claudinite/shared/packs/web/session-prepare.mjs"), "")
	got, _ = Content(repo, "0.0.0")
	if got != want+"@../temp/packs/current_user/RULES.md\n" {
		t.Fatalf("prepare: %q", got)
	}
}

func TestWriteAndCheck(t *testing.T) {
	repo := member(t)
	if st, _, _ := Check(repo, "0.0.0"); st != Absent {
		t.Fatalf("%s", st)
	}
	if changed, err := Write(repo, "0.0.0"); !changed || err != nil {
		t.Fatalf("%v %v", changed, err)
	}
	if st, _, _ := Check(repo, "0.0.0"); st != Current {
		t.Fatalf("%s", st)
	}
	if changed, _ := Write(repo, "0.0.0"); changed {
		t.Error("rewrote a current index")
	}
	write(t, filepath.Join(repo, File), "@x\n")
	if st, _, _ := Check(repo, "0.0.0"); st != Stale {
		t.Fatalf("%s", st)
	}
	empty := t.TempDir()
	write(t, filepath.Join(empty, ".claudinite/settings.yaml"), "packs:\n  declared: []\n")
	write(t, filepath.Join(empty, File), "@keep\n")
	if changed, _ := Write(empty, "0.0.0"); changed {
		t.Error("blanked the index of a declaration with nothing to import")
	}
}

func TestEnsureImport(t *testing.T) {
	repo := t.TempDir()
	if changed, err := EnsureImport(repo); !changed || err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ClaudeMD))
	if string(raw) != Import+"\n" {
		t.Fatalf("%q", raw)
	}
	write(t, filepath.Join(repo, ClaudeMD), "# mine\nno newline")
	_, _ = EnsureImport(repo)
	raw, _ = os.ReadFile(filepath.Join(repo, ClaudeMD))
	if string(raw) != "# mine\nno newline\n"+Import+"\n" || !HasImport(repo) {
		t.Fatalf("%q", raw)
	}
	if changed, _ := EnsureImport(repo); changed {
		t.Error("appended twice")
	}
}
