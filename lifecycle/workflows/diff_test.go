package workflows

import (
	"fmt"
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
	if _, err := os.Stat(filepath.Join(dir, ".github", "workflows", Superseded)); err == nil {
		t.Errorf("%s survives the patch", Superseded)
	}
}

// A member running the queue runs the update as the engine/update task, so
// the superseded update workflow is a deletion in the patch.
func TestDiffDeletesTheSupersededUpdateWorkflow(t *testing.T) {
	files := map[string]string{Superseded: string(SupersededTemplate())}
	for n, b := range Templates() {
		files[n] = string(b)
	}
	dir := writeMember(t, files)
	d, err := Diff(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("--- a/.github/workflows/%s\n+++ /dev/null\n@@ -1,%d +0,0 @@\n", Superseded, strings.Count(files[Superseded], "\n"))
	if !strings.HasPrefix(d, want) || strings.Count(d, "--- ") != 1 {
		t.Fatalf("want one deletion, got:\n%s", d)
	}
	applies(t, dir, d)
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
		"--- /dev/null\n+++ b/.github/workflows/claudinite-scheduler.yml\n@@ -0,0 +1,", "-# a member's comment\n"} {
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
	dir := writeMember(t, map[string]string{"claudinite-ci.yml": ci, Superseded: string(SupersededTemplate())})
	d, err := Diff(dir)
	if err != nil || !strings.Contains(d, "\\ No newline at end of file") {
		t.Fatalf("%v\n%s", err, d)
	}
	applies(t, dir, d)
}

func TestDiffKeepsTheMembersCronAndStampedSecrets(t *testing.T) {
	mine := ForRepo("o/r")
	exe := strings.Replace(string(mine["claudinite-executor.yml"]), SecretsMarker+"\n",
		SecretsMarker+"\n          ACME_TOKEN: ${{ secrets.ACME_TOKEN }}\n", 1)
	files := map[string]string{"claudinite-executor.yml": exe}
	for _, n := range Names {
		if _, set := files[n]; !set {
			files[n] = string(mine[n])
		}
	}
	if d, err := Diff(writeMember(t, files)); err != nil || d != "" {
		t.Errorf("a member's own cron and stamped secrets are not drift: %q %v", d, err)
	}
	files["claudinite-scheduler.yml"] = strings.Replace(files["claudinite-scheduler.yml"], "20 5,17 * * *", "0 * * * *", 1)
	if d, _ := Diff(writeMember(t, files)); !strings.Contains(d, "+    - cron: \""+CronPlaceholder+"\"") {
		t.Errorf("a cron this repo's hash did not write is drift: %q", d)
	}
}
