package pack

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
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
// discovered as engine tasks and it carries its rules and skills. A
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
	// pack-version-history's worker opens its own pull request.
	if !slices.Contains(fleet.Manifest.GitHubActions, "openPr") {
		t.Errorf("fleet pack grants %v, want openPr", fleet.Manifest.GitHubActions)
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

	plain := repoWith(t, "engine:\n  version: \"1.1.0\"\n")
	if s, err := packset.Load(plain, "0.0.0", false); err != nil || len(s.Packs) != 0 {
		t.Errorf("no fleet block: %+v %v", s.Packs, err)
	}
}
