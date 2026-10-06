package builtin

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
)

// tasksSettings declares the tasks pack beside the two folded packs.
var tasksSettings = strings.Replace(settingsYAML, "    - local/mypack\n", "    - claudinite-tasks\n    - local/mypack\n", 1)

// tasksRepo is a member declaring the tasks pack, with files merged over
// its pack descriptor.
func tasksRepo(files map[string]string) repo {
	base := map[string]string{".claudinite/shared/packs/claudinite-tasks/pack.json": "{\"version\": \"1\"}\n"}
	for k, v := range files {
		base[k] = v
	}
	return repo{settings: tasksSettings, base: base}
}

const goodTask = `{
  "id": "nightly",
  "description": "Recomputes the report.",
  "trigger": "schedule",
  "preconditions": ["due:daily"],
  "expected_outcome": "fresh_pr",
  "code_worker_mjs": "worker.mjs",
  "code_work_timeout": 60
}
`

const taskDir = ".claudinite/local/packs/mypack/tasks/nightly/"

func TestTaskDeclarationShape(t *testing.T) {
	t.Parallel()
	expect(t, tasksRepo(map[string]string{taskDir + "task.json": goodTask}).run(t, "task-declaration-shape"))

	bad := `{
  "id": "nightly",
  "frequency": "weekly",
  "trigger": "sometimes",
  "agent_model": "huge",
  "expected_outcome": "no_code_changes",
  "automerge": "anything",
  "precondition": "x",
  "preconditions": ["no-such-term"],
  "code_worker_mjs": "node worker.mjs",
  "code_work": "node ../escape.mjs"
}
`
	got := tasksRepo(map[string]string{taskDir + "task.json": bad}).run(t, "task-declaration-shape")
	text := ""
	for _, f := range got {
		text += f.Sentence + "\n"
	}
	for _, w := range []string{
		`declares "frequency", which is retired`,
		`"trigger" is "sometimes", not a legal value`,
		`"agent_model" is "huge", not a legal value`,
		`a "no_code_changes" task declares "automerge"`,
		`declares no "description"`,
		`declares "precondition", which is retired`,
		`"code_worker_mjs" is a command rather than a file name`,
		`both "code_work" and "code_worker_mjs" are declared`,
		`"code_work" reaches outside the task directory`,
		`a work step is declared but no numeric "code_work_timeout" is`,
	} {
		if !strings.Contains(text, w) {
			t.Errorf("no finding %q in:\n%s", w, text)
		}
	}
	for _, f := range got {
		if strings.Contains(f.Sentence, `no "description"`) && f.Class != findings.Advisory {
			t.Errorf("absent description is %s, want an advisory", f.Class)
		}
	}

	// A task-local term read off the sibling module as text: a schedule
	// task gating on an item-reading term is told to be a request task.
	local := strings.Replace(goodTask, `["due:daily"]`, `["due:daily", "my-gate"]`, 1)
	expect(t, tasksRepo(map[string]string{
		taskDir + "task.json":         local,
		taskDir + "preconditions.mjs": "// my-gate reads the item\nexport const terms = {\n  'my-gate': { needsItem: true, holds: () => true },\n};\n",
	}).run(t, "task-declaration-shape"),
		want{path: taskDir + "task.json", what: `a "schedule" task states a condition that reads the item itself`})

	// YAML spells the same declaration.
	expect(t, tasksRepo(map[string]string{taskDir + "task.yaml": "id: nightly\ndescription: Recomputes the report.\ntrigger: schedule\npreconditions: [\"due:daily\"]\nexpected_outcome: fresh_pr\ncode_worker_mjs: worker.mjs\ncode_work_timeout: 60\n"}).run(t, "task-declaration-shape"))
}

func TestTaskCodeWorkEnv(t *testing.T) {
	t.Parallel()
	expect(t, tasksRepo(map[string]string{
		taskDir + "worker.mjs":      "// CLAUDINITE_DRY_RUN is retired\nconst a = process.env.CLAUDINITE_DRY_RUN;\nconst b = process.env.CLAUDINITE_REPO;\nconst c = process.env.CLAUDINITE_DRY_RUN;\n",
		taskDir + "worker.test.mjs": "process.env.CLAUDINITE_OTHER = '1';\n",
	}).run(t, "task-code-work-env"),
		want{path: taskDir + "worker.mjs", what: "^reads `CLAUDINITE_DRY_RUN`, which code_work never sets$", fix: "CLAUDINITE_TARGET_PR and nothing else$"})
}

func TestExecutorWorkflowSecrets(t *testing.T) {
	t.Parallel()
	secretTask := strings.Replace(goodTask, `"code_work_timeout": 60`, `"code_work_timeout": 60, "code_work_required_secrets": ["B_TOKEN", "A_TOKEN"]`, 1)
	files := map[string]string{taskDir + "task.json": secretTask}
	expect(t, tasksRepo(files).run(t, "executor-workflow-secrets"),
		want{path: executorWorkflow, what: "^is missing, so nothing passes A_TOKEN, B_TOKEN to the executor$", advise: true})

	files[executorWorkflow] = "jobs:\n  run:\n    steps:\n      - env:\n          # claudinite:secrets\n          A_TOKEN: ${{ secrets.A_TOKEN }}\n"
	expect(t, tasksRepo(files).run(t, "executor-workflow-secrets"),
		want{path: executorWorkflow, line: 5, what: "^does not pass B_TOKEN, which", fix: "^add `B_TOKEN: \\$\\{\\{ secrets.B_TOKEN \\}\\}` to", advise: true})

	files[executorWorkflow] += "          B_TOKEN: ${{ secrets.B_TOKEN }}\n"
	expect(t, tasksRepo(files).run(t, "executor-workflow-secrets"))
}

func TestAutomergePolicyScope(t *testing.T) {
	t.Parallel()
	r := tasksRepo(map[string]string{"docs/a.md": "a\n", "src/a.go": "package a\n"})
	r.change = map[string]string{"docs/a.md": "b\n"}
	r.message = "docs\n\nClaudinite-Automerge-Policy: doc-changes"
	expect(t, r.run(t, "automerge-policy-scope"))

	r.change = map[string]string{"docs/a.md": "b\n", "src/a.go": "package a\n\nvar X = 1\n"}
	expect(t, r.run(t, "automerge-policy-scope"),
		want{path: "src/a.go", what: `^this branch armed auto-merge \(Claudinite-Automerge-Policy: doc-changes\) but `, fix: "^revert or split out src/a.go"})

	r.message = "no trailer"
	expect(t, r.run(t, "automerge-policy-scope"))
}

func TestTaskDeclarationMatchesFolder(t *testing.T) {
	t.Parallel()
	agentic := `{"id": "other", "agent_model": "opus", "agent_instructions": "spec.md"}`
	expect(t, repo{base: map[string]string{taskDir + "task.json": agentic}}.run(t, "task-declaration-matches-folder"),
		want{path: taskDir + "task.json", what: `^declares id "other" but its directory is "nightly"$`},
		want{path: taskDir + "task.json", what: `^declares agent_instructions "spec.md", which does not exist in ` + taskDir + `$`})
	expect(t, repo{base: map[string]string{taskDir + "task.json": `{"id": "nightly", "agent_instructions": "../x.md"}`}}.run(t, "task-declaration-matches-folder"),
		want{path: taskDir + "task.json", what: "reaches outside the task directory"})
	expect(t, repo{base: map[string]string{taskDir + "task.json": `{"id": "nightly", "agent_instructions": "task.md"}`, taskDir + "task.md": "x\n"}}.run(t, "task-declaration-matches-folder"))
}

func TestTaskMdOnlyWhenAgentic(t *testing.T) {
	t.Parallel()
	expect(t, repo{base: map[string]string{taskDir + "task.json": goodTask, taskDir + "task.md": "x\n"}}.run(t, "task-md-only-when-agentic"),
		want{path: taskDir + "task.md", what: "runs no agent", fix: "^rename it to " + taskDir + "README.md"})
	expect(t, repo{base: map[string]string{taskDir + "task.json": `{"agent_model": "opus"}`, taskDir + "task.md": "x\n"}}.run(t, "task-md-only-when-agentic"))
}

func TestTaskWorkerRestoresMain(t *testing.T) {
	t.Parallel()
	w := taskDir + "worker.sh"
	expect(t, repo{base: map[string]string{w: "#!/bin/sh\ngit commit -m x\ngit push\n"}}.run(t, "task-worker-restores-main"),
		want{path: w, what: "without ever returning the checkout to `main`$"})
	expect(t, repo{base: map[string]string{w: "#!/bin/sh\ngit push\ngit checkout main\n"}}.run(t, "task-worker-restores-main"),
		want{path: w, what: "only after it has already committed or pushed$"})
	expect(t, repo{base: map[string]string{w: "#!/bin/sh\n# git push first? no\ngit checkout main\ngit push\n"}}.run(t, "task-worker-restores-main"))
	// The retired local_packs spelling is no longer read.
	expect(t, repo{base: map[string]string{".claudinite/local_packs/mypack/tasks/nightly/worker.sh": "git push\n"}}.run(t, "task-worker-restores-main"))
}

// memberFlat is the member file the builtin tests' settings and packs
// produce.
const memberFlat = `{"version": 1, "settings": {"path": ".claudinite/settings.yaml", "format": "yaml"},
  "engine": {"package": "@claudinite/cli", "version": "0.0.0", "channel": "stable"},
  "packs": {"channel": "stable", "declared": [{"id": "claudinite-lifecycle"}, {"id": "claudinite-growth"}, {"id": "local/mypack"}]},
  "dormant": false, "held": {"claudinite-growth": "1", "claudinite-lifecycle": "1"}}
`

func TestFlatDeclarationsCurrent(t *testing.T) {
	t.Parallel()
	// No task anywhere: nothing to demand but the member file.
	expect(t, repo{base: map[string]string{"a.txt": "a\n", ".claudinite/cache/member.GENERATED.json": memberFlat}}.run(t, "flat-declarations-current"))
	expect(t, repo{base: map[string]string{"a.txt": "a\n"}}.run(t, "flat-declarations-current"),
		want{path: ".claudinite/cache/member.GENERATED.json", what: "is missing or unreadable$", fix: "cn tasks flat --write"})
	stale := strings.Replace(memberFlat, `"claudinite-growth": "1"`, `"claudinite-growth": "0.9"`, 1)
	expect(t, repo{base: map[string]string{"a.txt": "a\n", ".claudinite/cache/member.GENERATED.json": stale}}.run(t, "flat-declarations-current"),
		want{path: ".claudinite/cache/member.GENERATED.json", what: "no longer states what .claudinite/settings.yaml and the vendored pack manifests say$", fix: "cn tasks flat --write"})

	expect(t, repo{base: map[string]string{taskDir + "task.json": goodTask, ".claudinite/cache/member.GENERATED.json": memberFlat}}.run(t, "flat-declarations-current"),
		want{path: ".claudinite/cache/tasks.GENERATED.json", what: "is missing or unreadable$", fix: "cn tasks flat --write"})

	flat := "{\n  \"version\": 1,\n  \"tasks\": {\n    \"local/mypack/nightly\": {\n      \"path\": \"" + taskDir + "task.json\",\n      \"declaration\": {\"id\": \"nightly\"}\n    },\n    \"gone/x\": {\"path\": \"packs/gone/tasks/x/task.json\"}\n  }\n}\n"
	expect(t, repo{base: map[string]string{taskDir + "task.json": goodTask, ".claudinite/cache/tasks.GENERATED.json": flat, ".claudinite/cache/member.GENERATED.json": memberFlat}}.run(t, "flat-declarations-current"),
		want{path: ".claudinite/cache/tasks.GENERATED.json", what: "carries a copy of " + taskDir + "task.json that no longer matches it$"},
		want{path: ".claudinite/cache/tasks.GENERATED.json", what: `names "gone/x" at packs/gone/tasks/x/task.json, which is not a file here$`})

	// A member still holding the files under the legacy directory is
	// judged there until a converge moves them.
	expect(t, repo{base: map[string]string{taskDir + "task.json": goodTask, ".claudinite/flat/member.GENERATED.json": memberFlat}}.run(t, "flat-declarations-current"),
		want{path: ".claudinite/flat/tasks.GENERATED.json", what: "is missing or unreadable$"})
	legacyFlat := "{\n  \"version\": 1,\n  \"tasks\": {\n    \"local/mypack/nightly\": {\n      \"path\": \"" + taskDir + "task.json\",\n      \"declaration\": " + strings.TrimSpace(goodTask) + "\n    }\n  }\n}\n"
	expect(t, repo{base: map[string]string{taskDir + "task.json": goodTask, ".claudinite/flat/tasks.GENERATED.json": legacyFlat, ".claudinite/flat/member.GENERATED.json": memberFlat}}.run(t, "flat-declarations-current"))
	expect(t, repo{base: map[string]string{taskDir + "task.json": goodTask, ".claudinite/flat/tasks.GENERATED.json": flat, ".claudinite/flat/member.GENERATED.json": stale}}.run(t, "flat-declarations-current"),
		want{path: ".claudinite/flat/member.GENERATED.json", what: "no longer states what"},
		want{path: ".claudinite/flat/tasks.GENERATED.json", what: "carries a copy of " + taskDir + "task.json that no longer matches it$"},
		want{path: ".claudinite/flat/tasks.GENERATED.json", what: `names "gone/x" at packs/gone/tasks/x/task.json, which is not a file here$`})
}
