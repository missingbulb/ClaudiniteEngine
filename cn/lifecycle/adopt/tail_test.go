package adopt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// asksManifest is a pack that asks two questions, seeds a file, hands a
// person a step and declares a task needing a secret.
const asksManifest = `, "questions": [{"id": "goals", "prompt": "What is the\n project for?", "distill": "one line"}, {"id": "tone", "prompt": "Which tone?"}],
  "seedOps": [{"template": "templates/seeded.yml", "dest": ".github/seeded.yml"}],
  "adoptionHandover": [{"step": "Add the HELLO_TOKEN secret", "breaks": "the greeting task parks", "done": "a greeting lands"}]`

func withAsks(t *testing.T) *fakePacks {
	f := newPacks(t)
	f.publishFiles("asks", "1.0", "stable", asksManifest, map[string][]byte{
		"templates/seeded.yml":  []byte("# seeded\nkey: value\n"),
		"tasks/greet/task.json": []byte(`{"code_work_required_secrets": ["HELLO_TOKEN"]}`),
	})
	return f
}

func TestInitAsksSeedsStampsAndHandsOver(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "hello", "asks")
	in.Reader = withAsks(t)
	in.Answers = []AnswerFlag{{Address: "asks/tone", Text: "dry"}}
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s := out.String()
	for _, want := range []string{
		"answered asks/tone\n",
		"seeded .github/seeded.yml\n",
		"stamped HELLO_TOKEN into .github/workflows/claudinite-executor.yml\n",
		"\nQUESTIONS — 1 adoption question(s) unanswered; ask them in one AskUserQuestion pass and record each with cn settings answer:\n  asks/goals: What is the project for?\n    distill: one line\n",
		"  [ ] (asks) Add the HELLO_TOKEN secret\n        while off: the greeting task parks\n        done when: a greeting lands\n",
		"HANDOVER — 2 step(s)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	if !strings.HasSuffix(s, "NEXT: commit everything above and open one pull request, which a person merges; then file the HANDOVER block as one issue.\n") {
		t.Errorf("NEXT is not the last line:\n%s", s)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".github/seeded.yml")); string(b) != "# seeded\nkey: value\n" {
		t.Errorf("seed %q", b)
	}
	exe, _ := os.ReadFile(filepath.Join(repo, ExecutorWorkflow))
	if got := workflows.StampedSecrets(exe); strings.Join(got, ",") != "HELLO_TOKEN" {
		t.Errorf("stamped %v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	p, err := settings.ParseFile(raw, settings.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := p.Packs.Entry("asks", false); e.Answers["tone"] != "dry" {
		t.Errorf("answer not recorded:\n%s", raw)
	}
}

func TestInitWithEveryAnswerPrintsNoQuestions(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "asks")
	in.Reader = withAsks(t)
	in.Answers = []AnswerFlag{{"asks/tone", "dry"}, {"asks/goals", "n/a"}}
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out.String(), "QUESTIONS") {
		t.Errorf("a QUESTIONS block with nothing pending:\n%s", out)
	}
}

func TestAdoptSeveralAndKeepTheMembersOwn(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "base")
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	exePath := filepath.Join(repo, ExecutorWorkflow)
	exe, _ := os.ReadFile(exePath)
	mine, _ := workflows.Stamp(exe, []string{"MY_OWN"})
	_ = os.WriteFile(exePath, mine, 0o644)
	_ = os.MkdirAll(filepath.Join(repo, ".github"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".github/seeded.yml"), []byte("mine\n"), 0o644)

	var aout bytes.Buffer
	if err := Adopt(AdoptInput{Repo: repo, IDs: []string{"hello", "asks"}, Reader: withAsks(t), Out: &aout}); err != nil {
		t.Fatalf("%v\n%s", err, aout.String())
	}
	s := aout.String()
	if !strings.Contains(s, "pack: hello 1.0\npack: asks 1.0\n") || strings.Contains(s, "pack: base") {
		t.Errorf("packs:\n%s", s)
	}
	if strings.Contains(s, "seeded ") {
		t.Errorf("seeded over the member's file:\n%s", s)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".github/seeded.yml")); string(b) != "mine\n" {
		t.Errorf("the member's file moved: %q", b)
	}
	exe, _ = os.ReadFile(exePath)
	if got := workflows.StampedSecrets(exe); strings.Join(got, ",") != "HELLO_TOKEN,MY_OWN" {
		t.Errorf("stamped %v", got)
	}
	if !strings.Contains(s, "QUESTIONS — 2 ") || !strings.Contains(s, "HANDOVER — 1 step(s)") || strings.Contains(s, "(cn)") {
		t.Errorf("adopt's blocks name only what it added:\n%s", s)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	if p, _ := settings.ReadPacks(raw, settings.YAML); strings.Join(p.Declared, ",") != "base,hello,asks" {
		t.Errorf("declared %v", p.Declared)
	}
}

func TestAdoptResolvesEveryIDBeforeWriting(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "base")
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	before, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	var aout bytes.Buffer
	if err := Adopt(AdoptInput{Repo: repo, IDs: []string{"asks", "nope"}, Reader: withAsks(t), Out: &aout}); err == nil {
		t.Fatal("adopted an id the index does not hold")
	}
	after, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	if !bytes.Equal(before, after) {
		t.Errorf("settings written before every id resolved:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claudinite/shared/packs/asks")); err == nil {
		t.Error("asks vendored before nope resolved")
	}
}

func TestAnswerRefuses(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "asks")
	in.Reader = withAsks(t)
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for addr, want := range map[string]string{
		"nope/goals": "does not declare pack nope",
		"asks/nope":  "it asks goals, tone",
		"asks":       "want <pack>/<question>",
	} {
		if _, err := Answer(repo, ver, addr, "x"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", addr, err)
		}
	}
	if _, err := Answer(repo, ver, "asks/goals", "  "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty: %v", err)
	}
	if _, err := Answer(repo, ver, "asks/goals", "n/a"); err != nil {
		t.Errorf("n/a is a text: %v", err)
	}
}

func TestHandoverGolden(t *testing.T) {
	two := packOf("two", `{"adoptionHandover": [{"step": "A", "breaks": "b", "done": "c"}, {"step": "D", "breaks": "e", "done": "f"}]}`)
	none := packOf("none", `{"id": "none"}`)
	tasks := packOf("claudinite-tasks", `{"id": "claudinite-tasks"}`)
	var buf bytes.Buffer
	writeHandover(&buf, Handover(HandoverInput{Core: true, Tasks: true, Newly: []packset.Pack{two, none, tasks}}))
	want := `
HANDOVER — 4 step(s) only a human can do; file them as ONE issue, a checkbox each, never a note in the PR body:
  [ ] (cn) In the repository's Settings > Actions > General, allow GitHub Actions to create and approve pull requests
        while off: the nightly update and every task that opens a pull request fail at the open
        done when: the first engine/update item's pull request exists
  [ ] (claudinite-tasks) Mint the executor routine's bearer token and add it as the Actions secret CCR_ROUTINE_TOKEN
        while off: every item with an agentic phase parks needs-human-action naming the secret
        done when: an executor run fires a routine
  [ ] (two) A
        while off: b
        done when: c
  [ ] (two) D
        while off: e
        done when: f
`
	if buf.String() != want {
		t.Errorf("got\n%s", buf.String())
	}
	buf.Reset()
	writeHandover(&buf, Handover(HandoverInput{Core: true, Newly: []packset.Pack{none}}))
	if strings.Count(buf.String(), "[ ]") != 1 {
		t.Errorf("one core row:\n%s", buf.String())
	}
	buf.Reset()
	writeNext(&buf, NextInput{First: []string{"git rm old-config.json"}, Routine: true, Handover: true})
	if buf.String() != "\nNEXT: git rm old-config.json; then create the executor routine with create_trigger, its stored prompt \"Read `.claudinite/cache/instructions.md` and follow it.\", and record its endpoint on the claudinite-tasks entry's config.agenticTaskInvocationEndpoints; then commit everything above and open one pull request, which a person merges; then file the HANDOVER block as one issue.\n" {
		t.Errorf("next %q", buf.String())
	}
}

func packOf(id, manifest string) packset.Pack {
	m, err := packset.ParseManifest([]byte(`{"version": "1.0", ` + manifest[1:]))
	if err != nil {
		panic(err)
	}
	return packset.Pack{ID: id, Kind: packset.Canon, Manifest: m}
}
