package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeMember(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, ".github", "workflows", name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// applies checks the diff is a patch that turns the member's workflows
// into the templates.
func applies(t *testing.T, dir, diff string) {
	t.Helper()
	cmd := exec.Command("patch", "-p1", "--batch", "--silent")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(diff)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("patch: %v\n%s\n%s", err, out, diff)
	}
	for name, want := range Templates() {
		got, _ := os.ReadFile(filepath.Join(dir, ".github", "workflows", name))
		if string(got) != string(want) {
			t.Errorf("%s after the patch:\n%s", name, got)
		}
	}
}

func TestDiffIsEmptyForTheTemplates(t *testing.T) {
	files := map[string]string{}
	for n, b := range Templates() {
		files[n] = string(b)
	}
	if d, err := Diff(writeMember(t, files)); err != nil || d != "" {
		t.Errorf("%q %v", d, err)
	}
}

func TestDiffPatchesChangedAndMissingFiles(t *testing.T) {
	tpl := Templates()
	ci := string(tpl["claudinite-ci.yml"])
	edited := strings.Replace(ci, "fetch-depth: 0", "fetch-depth: 1", 1)
	edited = "# a member's comment\n" + edited + "# trailing\n"
	dir := writeMember(t, map[string]string{"claudinite-ci.yml": edited})
	d, err := Diff(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"--- a/.github/workflows/claudinite-ci.yml\n+++ b/.github/workflows/claudinite-ci.yml\n@@ ",
		"--- /dev/null\n+++ b/.github/workflows/claudinite-update.yml\n@@ -0,0 +1,", "-# a member's comment\n"} {
		if !strings.Contains(d, s) {
			t.Errorf("diff lacks %q:\n%s", s, d)
		}
	}
	if strings.Count(d, "@@ -") < 3 {
		t.Errorf("distant edits share one hunk:\n%s", d)
	}
	applies(t, dir, d)
}

func TestDiffHandlesAFileWithoutATrailingNewline(t *testing.T) {
	ci := strings.TrimSuffix(string(Templates()["claudinite-ci.yml"]), "\n")
	dir := writeMember(t, map[string]string{"claudinite-ci.yml": ci, "claudinite-update.yml": string(Templates()["claudinite-update.yml"])})
	d, err := Diff(dir)
	if err != nil || !strings.Contains(d, "\\ No newline at end of file") {
		t.Fatalf("%v\n%s", err, d)
	}
	applies(t, dir, d)
}
