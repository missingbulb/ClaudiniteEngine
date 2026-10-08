package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
)

func repoWith(t *testing.T, settings string) string {
	t.Helper()
	repo := t.TempDir()
	p := filepath.Join(repo, ".claudinite/settings.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// A fleet manager runs the fleet pack the binary carries: its tasks are
// discovered as engine tasks and its rules reach the session's index. A
// repo with no fleet block carries none of it.
func TestAFleetManagerRunsTheFleetPack(t *testing.T) {
	repo := repoWith(t, "engine:\n  version: \"1.1.0\"\nfleet:\n  owner: acme\n")
	s, err := packset.Load(repo, "0.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	var fleet *packset.Pack
	for i := range s.Packs {
		if s.Packs[i].ID == packset.FleetPack {
			fleet = &s.Packs[i]
		}
	}
	if fleet == nil || fleet.Kind != packset.Engine || fleet.ProsePath() == "" || len(fleet.Skills) == 0 {
		t.Fatalf("fleet pack: %+v", fleet)
	}
	tasks, errs := taskspec.Discover(repo, s.Packs)
	if len(errs) > 0 {
		t.Fatalf("discovery: %v", errs)
	}
	want, _ := os.ReadDir("files/tasks")
	got := map[string]bool{}
	for _, task := range tasks {
		if task.Pack == packset.FleetPack && task.Engine {
			got[task.ID] = true
		}
	}
	for _, d := range want {
		if !got[d.Name()] {
			t.Errorf("task %s not discovered: %v", d.Name(), got)
		}
	}
	if _, err := rulesindex.Converge(repo, "0.0.0"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, rulesindex.File))
	if !strings.Contains(string(raw), "@../temp/packs/fleet/RULES.md\n") {
		t.Errorf("rules index: %q", raw)
	}

	plain := repoWith(t, "engine:\n  version: \"1.1.0\"\n")
	if s, err := packset.Load(plain, "0.0.0", false); err != nil || len(s.Packs) != 0 {
		t.Errorf("no fleet block: %+v %v", s.Packs, err)
	}
}
