package workflows

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Stage writes exactly the workflows that differ from what this binary
// expects, under the staging directory and never under .github/workflows/,
// and clears what an earlier stage left there.
func TestStageWritesOnlyWhatDiffers(t *testing.T) {
	const gcec = "missingbulb/GoogleCalendarEventCreator"
	files := map[string]string{}
	for n, b := range ForRepo(gcec) {
		files[n] = string(b)
	}
	files["claudinite-scheduler.yml"] = strings.Replace(files["claudinite-scheduler.yml"], "    - cron: \"24 4,16 * * *\"\n", "", 1)
	dir := writeMember(t, files)
	stale := filepath.Join(dir, filepath.FromSlash(StagedPath("claudinite-ci.yml")))
	_ = os.MkdirAll(filepath.Dir(stale), 0o755)
	_ = os.WriteFile(stale, []byte("left by an earlier stage\n"), 0o644)

	staged, err := Stage(dir, gcec)
	if err != nil || !reflect.DeepEqual(staged, []string{".claudinite/cache/pending-workflows/claudinite-scheduler.yml"}) {
		t.Fatalf("%v %v", staged, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(staged[0])))
	if string(got) != string(ForRepo(gcec)["claudinite-scheduler.yml"]) {
		t.Errorf("staged:\n%s", got)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file an earlier stage left survived")
	}
	live, _ := os.ReadFile(filepath.Join(dir, ".github", "workflows", "claudinite-scheduler.yml"))
	if string(live) != files["claudinite-scheduler.yml"] {
		t.Error("Stage wrote .github/workflows/")
	}

	if _, err := Stage(dir, ""); err == nil {
		t.Error("staged a cronless scheduler without the repo's name")
	}
	files["claudinite-scheduler.yml"] = string(ForRepo(gcec)["claudinite-scheduler.yml"])
	dir = writeMember(t, files)
	if staged, err := Stage(dir, gcec); err != nil || len(staged) != 0 {
		t.Errorf("workflows already expected staged %v %v", staged, err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(StagingDir))); err == nil {
		t.Error("an empty stage left the directory")
	}
}
