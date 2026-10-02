package taskspec

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

func put(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const agentless = `{"id": "%s", "trigger": "schedule", "preconditions": ["due:daily"], "expected_outcome": "fresh_pr", "code_worker_mjs": "worker.mjs", "code_work_timeout": 60}`

func decl(id string) string { return strings.Replace(agentless, "%s", id, 1) }

func TestDiscover(t *testing.T) {
	repo := t.TempDir()
	put(t, repo, map[string]string{
		".claudinite/local/packs/acme-pack/tasks/nightly/task.json":         decl("nightly"),
		".claudinite/local/packs/acme-pack/tasks/weekly/task.yaml":          "id: weekly\ntrigger: request\nexpected_outcome: no_code_changes\ncode_worker_mjs: worker.mjs\ncode_work_timeout: 5\n",
		".claudinite/local/packs/acme-pack/tasks/renamed/task.json":         decl("other"),
		".claudinite/local/packs/acme-pack/tasks/broken/task.json":          "{",
		".claudinite/local/packs/acme-pack/tasks/invalid/task.json":         `{"id": "invalid"}`,
		".claudinite/local/packs/acme-pack/tasks/twice/task.json":           decl("twice"),
		".claudinite/local/packs/acme-pack/tasks/twice/task.toml":           "id = \"twice\"\n",
		".claudinite/local/packs/acme-pack/tasks/notes/README.md":           "no declaration\n",
		".claudinite/temp/packs/current_user/tasks/mine/task.json":          decl("mine"),
		".claudinite/local/packs/acme-pack/tasks/gated/task.json":           strings.Replace(decl("gated"), `["due:daily"]`, `["due:daily", "my-gate"]`, 1),
		".claudinite/local/packs/acme-pack/tasks/gated/preconditions.mjs":   "export const terms = {\n  'my-gate': { signals: ['commits'], holds: () => true },\n};\n",
	})
	packs := []packset.Pack{
		{ID: "acme-pack", Kind: packset.Local, Dir: filepath.Join(repo, ".claudinite/local/packs/acme-pack"), Rel: ".claudinite/local/packs/acme-pack"},
		{ID: "current_user", Kind: packset.Temp, Dir: filepath.Join(repo, ".claudinite/temp/packs/current_user"), Rel: ".claudinite/temp/packs/current_user"},
	}
	tasks, errs := Discover(repo, packs)
	var got []string
	for _, t := range tasks {
		got = append(got, t.Path())
	}
	if want := []string{"acme-pack/gated", "acme-pack/nightly", "acme-pack/weekly", "engine/implement-request"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tasks %v, want %v", got, want)
	}
	signals := Signals(tasks[0].Decl.Preconditions(), tasks[0].Terms)
	sort.Strings(signals)
	if tasks[0].Rel != ".claudinite/local/packs/acme-pack/tasks/gated" || !reflect.DeepEqual(signals, []string{"commits", "runs"}) {
		t.Errorf("gated: %s, signals %v", tasks[0].Rel, Signals(tasks[0].Decl.Preconditions(), tasks[0].Terms))
	}
	if m := tasks[2].Decl.AgentModel(); m != "none" {
		t.Errorf("defaults not applied: agent_model %q", m)
	}
	whats := map[string]string{}
	for _, e := range errs {
		whats[e.Task] = e.What
	}
	for task, want := range map[string]string{
		"renamed": `declares id "other" but its directory is "renamed"`,
		"broken":  "failed to load",
		"invalid": "is not a valid task declaration",
		"twice":   "more than one spelling",
	} {
		if !strings.Contains(whats[task], want) {
			t.Errorf("%s: %q, want %q", task, whats[task], want)
		}
	}
	if len(errs) != 4 {
		t.Errorf("%d errors, want 4: %v", len(errs), errs)
	}
}

func TestTermsFromText(t *testing.T) {
	terms := TermsFromText(`export const terms = {
  "first": { needsItem: true, signals: ["issues", 'prs'] },
  'second': {
    takesArg: true,
  },
  'first': { takesArg: true },
};`)
	if len(terms) != 2 || terms[0].Name != "first" || !terms[0].TakesArg || terms[0].NeedsItem || terms[1].Name != "second" || !terms[1].TakesArg {
		t.Errorf("terms %+v", terms)
	}
	if TermsFromText("export const other = {};\n") != nil {
		t.Error("a file exporting no terms has terms")
	}
}

func TestSecretNames(t *testing.T) {
	got := SecretNames([]Decl{
		{"code_work_required_secrets": []any{"B", "A"}},
		{"code_work_required_secrets": []any{"A", "C"}},
		{},
	})
	if !reflect.DeepEqual(got, []string{"A", "B", "C"}) {
		t.Errorf("%v", got)
	}
}
