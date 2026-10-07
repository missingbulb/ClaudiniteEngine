package rulesindex

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
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
// to the index, the person's literal line last where the user-pack pack is
// declared, and never for a pack that merely ships Node's prepare step.
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
	if got, _ = Content(repo, "0.0.0"); got != want {
		t.Fatalf("a prepare step alone imports nothing: %q", got)
	}
	write(t, filepath.Join(repo, ".claudinite/settings.yaml"), "packs:\n  declared:\n    - web\n    - basics\n    - local/mine\n    - quiet\n    - claude-code-web-users-support\n")
	write(t, filepath.Join(repo, ".claudinite/shared/packs/claude-code-web-users-support/pack.json"), `{"version": "1.0", "prose": null}`)
	if got, _ = Content(repo, "0.0.0"); got != want+"@../temp/packs/current_user/RULES.md\n" {
		t.Fatalf("the user-pack pack declared: %q", got)
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

// A CRLF CLAUDE.md keeps its line endings: the import line is appended
// with CRLF, and is read back as present.
func TestWithImportKeepsCRLF(t *testing.T) {
	cases := map[string]string{
		"# Notes\r\nkeep\r\n": "# Notes\r\nkeep\r\n" + Import + "\r\n",
		"# Notes\r\nkeep":     "# Notes\r\nkeep\r\n" + Import + "\r\n",
		"# Notes\nkeep\n":     "# Notes\nkeep\n" + Import + "\n",
		"":                    Import + "\n",
	}
	for in, want := range cases {
		got := WithImport([]byte(in))
		if string(got) != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
		if !HasImportIn(got) {
			t.Errorf("%q: the appended import is not read back", in)
		}
	}
}

// A member still holding the generated files under the legacy directory
// is moved by a converge: every file lands under the new directory, the
// old one is gone, CLAUDE.md imports the new index in the line's place,
// and every path the move touched is reported for the commit.
func TestConvergeMovesTheLegacyDirectory(t *testing.T) {
	repo := member(t)
	if _, err := Converge(repo, "0.0.0"); err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(repo, File))
	legacy := filepath.Join(repo, filepath.FromSlash(flatdecl.LegacyDir))
	if err := os.Rename(filepath.Join(repo, filepath.FromSlash(flatdecl.Dir)), legacy); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, ClaudeMD), "# mine\r\n"+LegacyImport+"\r\nkeep\r\n")
	if !NeedsMove(repo) {
		t.Fatal("NeedsMove is false on a legacy member")
	}
	if st, _, _ := Check(repo, "0.0.0"); st != Current {
		t.Errorf("the legacy index is %s where the member holds it", st)
	}

	got, err := Converge(repo, "0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{File, flatdecl.LegacyPath(File), flatdecl.TasksFile, flatdecl.LegacyPath(flatdecl.TasksFile), ClaudeMD} {
		if !slices.Contains(got, f) {
			t.Errorf("Converge reported %v, without %s", got, f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(repo, File)); string(b) != string(want) {
		t.Errorf("the moved index: %q", b)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("the legacy directory survived: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ClaudeMD)); string(b) != "# mine\r\n"+Import+"\r\nkeep\r\n" {
		t.Errorf("CLAUDE.md: %q", b)
	}
	if NeedsMove(repo) {
		t.Error("NeedsMove after the move")
	}
	if again, _ := Converge(repo, "0.0.0"); len(again) != 0 {
		t.Errorf("a second converge changed %v", again)
	}
}

// RepointImport drops the legacy line where the file already imports the
// new index, and leaves a file without it as it is.
func TestRepointImport(t *testing.T) {
	for in, want := range map[string]string{
		"x\n":                                     "x\n",
		LegacyImport + "\n" + Import + "\n":       Import + "\n",
		"  " + LegacyImport + "\n":                "  " + Import + "\n",
		LegacyImport + "\n" + LegacyImport + "\n": Import + "\n",
	} {
		if got := string(RepointImport([]byte(in))); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
