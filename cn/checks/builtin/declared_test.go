package builtin

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
)

// declaredRun runs the world-scoped declared check id over the repo and
// returns its findings.
func (r repo) declaredRun(t *testing.T, id string) ([]findings.Finding, bool) {
	t.Helper()
	set, err := declared.LoadSet(r.build(t), "0.0.0", All()...)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Faults) > 0 {
		t.Fatalf("faults %v", set.Faults)
	}
	loaded := false
	for _, c := range set.Checks {
		if c.ID == id {
			loaded = true
		}
	}
	fs, _ := set.Run(declared.Selection{Tags: []string{"world"}}, pastGrace, nil)
	var out []findings.Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out, loaded
}

// The queue's declared checks run on every member, with no declared-checks
// file in its mount.
func TestTheQueuesDeclaredChecksRunOnEveryMember(t *testing.T) {
	t.Parallel()
	prose := map[string]string{".claudinite/local/packs/mypack/RULES.md": "- File it and mark it for the queue.\n"}
	got, loaded := repo{base: prose}.declaredRun(t, "queue-mark-named-literally")
	if !loaded || len(got) != 1 || got[0].Path != ".claudinite/local/packs/mypack/RULES.md" {
		t.Fatalf("loaded %v, findings %v", loaded, got)
	}
}

// A pack whose own file still declares an id the engine carries, as an
// older claudinite-tasks mount does, runs the engine's copy alone.
func TestAPacksOwnCopyOfAnEngineDeclaredCheckGivesWay(t *testing.T) {
	t.Parallel()
	base := map[string]string{
		".claudinite/shared/packs/claudinite-tasks/pack.json": "{\"version\": \"1\"}\n",
		".claudinite/shared/packs/claudinite-tasks/declared-checks.json": `[{"id": "queue-mark-named-literally", "on_fail": "block", "since": "2026-09-22",
  "failureMessage": "old", "fix": "old", "scanFiles": "/RULES\\.md$/", "matchLines": [{"match": "/./", "what": "old copy", "fix": "old"}]}]
`,
		".claudinite/local/packs/mypack/RULES.md": "- Nothing to see.\n",
	}
	settings := strings.Replace(settingsYAML, "    - local/mypack\n", "    - claudinite-tasks\n    - local/mypack\n", 1)
	set, err := declared.LoadSet(repo{settings: settings, base: base}.build(t), "0.0.0", All()...)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range set.Checks {
		if c.ID == "queue-mark-named-literally" {
			n++
			if c.File != declared.EngineDeclarationFile(declared.EnginePack) {
				t.Errorf("ran the copy in %s", c.File)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d copies loaded", n)
	}
}

func TestEveryEngineDeclaredCheckCompiles(t *testing.T) {
	t.Parallel()
	packs := declared.EngineDeclarationPacks()
	if len(packs) == 0 {
		t.Fatal("the engine carries no declared checks")
	}
	for _, p := range packs {
		cs, err := declared.LoadEngine(p)
		if err != nil || len(cs) == 0 {
			t.Errorf("%s: %d checks, %v", p, len(cs), err)
		}
	}
}
