package builtin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
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

// The queue's label guard passes a project's own labels and the queue's
// vocabulary, and flags a label beside the queue mark or in its place.
func TestTheQueueLabelGuard(t *testing.T) {
	t.Parallel()
	cs, err := declared.LoadEngine(declared.EnginePack)
	if err != nil {
		t.Fatal(err)
	}
	var guard *declared.Check
	for _, c := range cs {
		if c.ID == "issue-label-outside-the-queue-vocabulary" {
			guard = c
		}
	}
	if guard == nil {
		t.Fatal("the engine carries no label guard")
	}
	write := func(method string, names ...string) declared.Call {
		raw, _ := json.Marshal(map[string]any{"method": method, "owner": "o", "repo": "r", "labels": names})
		in, err := jsjson.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		return declared.Call{Tool: "mcp__github__issue_write", Input: in}
	}
	for _, c := range []struct {
		name  string
		calls []declared.Call
		want  int
	}{
		{"a project's own labels", []declared.Call{write("update", "acme-request", "needs-human"), write("create", "bug")}, 0},
		{"the queue's own vocabulary", []declared.Call{write("create", "task:origin:ad-hoc"), write("update", "task:status:done", "outcome:done")}, 0},
		{"a label invented beside the queue mark", []declared.Call{write("create", "task:origin:ad-hoc", "acme-backlog")}, 1},
		{"a queue-named label in place of the mark", []declared.Call{write("create", "claudinite-queue"), write("update", "acme-backlog"), write("create", "claude-queued")}, 2},
	} {
		n := 0
		for i, call := range c.calls {
			hs, err := declared.GuardFindings(guard, call, c.calls[:i])
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			n += len(hs)
		}
		if n != c.want {
			t.Errorf("%s: %d findings, want %d", c.name, n, c.want)
		}
	}
}
