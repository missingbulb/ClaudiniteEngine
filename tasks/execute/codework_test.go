package execute

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
)

func TestADeclaredSecretIsMissingOnlyWhenNeitherTheBagNorTheJobCarriesIt(t *testing.T) {
	env := map[string]string{"CLAUDINITE_SECRETS": `{"IN_BAG":"b","EMPTY":""}`, "IN_ENV": "e"}
	got := MissingSecrets([]string{"IN_BAG", "IN_ENV", "EMPTY", "ABSENT", "CLAUDINITE_SECRETS"}, env)
	if !reflect.DeepEqual(got, []string{"ABSENT", "CLAUDINITE_SECRETS"}) {
		t.Error(got)
	}
	if got := MissingSecrets(nil, env); len(got) != 0 {
		t.Error(got)
	}
}

func TestTheTaskEnvironmentSelectsSecretsAndCarriesEveryVariable(t *testing.T) {
	env := map[string]string{
		"CLAUDINITE_SECRETS": `{"WANTED":"w","OTHER":"o"}`,
		"CLAUDINITE_VARS":    `{"SITE":"x","PATH":"overridden?"}`,
		"OTHER":              "o-from-env",
		"PATH":               "/bin",
		"GITHUB_TOKEN":       "ghs",
	}
	got := TaskEnv([]string{"WANTED"}, env)
	want := map[string]string{"WANTED": "w", "SITE": "x", "PATH": "/bin", "GITHUB_TOKEN": "ghs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
	// With no bag the stamped names stay inherited.
	if got := TaskEnv(nil, map[string]string{"STAMPED": "s"}); got["STAMPED"] != "s" {
		t.Error(got)
	}
	// A malformed bag is no bag.
	if got := TaskEnv(nil, map[string]string{"CLAUDINITE_SECRETS": "[1]", "X": "y"}); !reflect.DeepEqual(got, map[string]string{"X": "y"}) {
		t.Error(got)
	}
}

func TestTheCodeWorkVariablesAreExactlyTheContractsNames(t *testing.T) {
	tk := agentless("a")
	env := CodeWorkEnv(CodeWorkPlace{Root: "/r", Repo: "o/r", DefaultBranch: "main"}, tk, workitem.Issue{Number: 4},
		[]string{"one", "two"}, "/tmp/req", Target{Mode: ModeAmend, Branch: "b", PR: 9})
	var keys []string
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := append([]string{}, taskspec.CodeWorkEnvVars...)
	slices.Sort(want)
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("%v\n%v", keys, want)
	}
	if env["CLAUDINITE_ITEM"] != "4" || env["CLAUDINITE_CONTEXT"] != "one\ntwo" || env["CLAUDINITE_TARGET_PR"] != "9" || env["CLAUDINITE_PACK"] != "acme-pack" {
		t.Error(env)
	}
}

func TestTheLastTriageMarkerWins(t *testing.T) {
	out := "x\nclaudinite-needs-human: decision - first\nmore\nclaudinite-needs-human: action — PAT lacks Actions: write\n"
	if got := ReadTriage(out); got == nil || got.Kind != "action" || got.Detail != "PAT lacks Actions: write" {
		t.Errorf("%+v", got)
	}
	if got := ReadTriage("claudinite-needs-human: failure\n"); got == nil || got.Kind != "failure" || got.Detail != "" {
		t.Errorf("%+v", got)
	}
	if ReadTriage("nothing here\n  claudinite-needs-human: action\n") != nil {
		t.Error("a marker must open its line")
	}
}

func TestTheRequeueMarkerNormalizesItsInstant(t *testing.T) {
	got := ReadRequeue("claudinite-requeue: 2026-09-01T00:00:00Z - early\nclaudinite-requeue: 2026-09-01T12:00:00+02:00 — not yet live\n")
	if got == nil || got.Until != "2026-09-01T10:00:00.000Z" || got.Reason != "not yet live" {
		t.Errorf("%+v", got)
	}
	if got := ReadRequeue("claudinite-requeue: 2026-09-02\n"); got == nil || got.Until != "2026-09-02T00:00:00.000Z" {
		t.Errorf("%+v", got)
	}
	if got := ReadRequeue("claudinite-requeue: tomorrow-ish because\n"); got == nil || got.Until != "" || got.Reason != "because" {
		t.Errorf("%+v", got)
	}
	if ReadRequeue("no marker\n") != nil {
		t.Error("no marker")
	}
}

func TestTheAgentRequestNamesWhatTheRunCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "req")
	if r := readAgentRequest(path); r != nil {
		t.Error("absent is no request")
	}
	_ = os.WriteFile(path, []byte("\n"), 0o600)
	if r := readAgentRequest(path); r == nil || r.PR != 0 {
		t.Errorf("a bare marker requests and names nothing: %+v", r)
	}
	_ = os.WriteFile(path, []byte(`{"delivered":{"branch":"b","pr":7,"merged":true,"issue":3},"reason":{"code":"gate","detail":"2 findings"}}`), 0o600)
	if r := readAgentRequest(path); r == nil || r.PR != 7 || !r.Merged || r.Branch != "b" || r.Issue != 3 || r.Reason != "2 findings" {
		t.Errorf("%+v", r)
	}
	_ = os.WriteFile(path, []byte(`{"reason":{"code":"gate"}}`), 0o600)
	if r := readAgentRequest(path); r == nil || r.Reason != "gate" {
		t.Errorf("%+v", r)
	}
	_ = os.WriteFile(path, []byte(`not json`), 0o600)
	if r := readAgentRequest(path); r == nil || r.PR != 0 {
		t.Errorf("%+v", r)
	}
}

func shellTask(t *testing.T, command string, decl map[string]any) taskspec.Task {
	t.Helper()
	d := map[string]any{"agent_model": "none", "code_work": command, "code_work_timeout": 10}
	for k, v := range decl {
		d[k] = v
	}
	tk := loopTask("a", d)
	tk.Dir = t.TempDir()
	return tk
}

func worker(t *testing.T) CodeWorker {
	return CodeWorker{Place: CodeWorkPlace{Root: "/r", Repo: "o/r", DefaultBranch: "main"},
		Env: map[string]string{"PATH": os.Getenv("PATH")}, TempDir: t.TempDir(), Log: func(string) {}}
}

func TestAShellWorkerRunsInItsTaskFolderWithTheContractsVariables(t *testing.T) {
	tk := shellTask(t, `pwd > seen; env | grep '^CLAUDINITE_' | sort >> seen`, nil)
	res := worker(t).Run(tk, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeFresh, Branch: "b"}})
	if !res.OK {
		t.Fatalf("%+v", res)
	}
	raw, _ := os.ReadFile(filepath.Join(tk.Dir, "seen"))
	for _, want := range []string{tk.Dir + "\n", "CLAUDINITE_ITEM=4\n", "CLAUDINITE_TARGET_MODE=fresh\n", "CLAUDINITE_TARGET_PR=\n", "CLAUDINITE_REPO=o/r\n"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("lacks %q:\n%s", want, raw)
		}
	}
}

func TestAShellWorkerThatRequestsTheAgentHandsOverWhatItCreated(t *testing.T) {
	tk := shellTask(t, `printf '{"delivered":{"pr":7,"branch":"b"},"reason":{"detail":"why"}}' > "$CLAUDINITE_REQUEST_AGENT"`, nil)
	w := worker(t)
	res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}})
	if !res.OK || !res.AgentRequested || res.DeliveredPR != 7 || res.Branch != "b" || res.Reason != "why" {
		t.Fatalf("%+v", res)
	}
	entries, _ := os.ReadDir(w.TempDir)
	if len(entries) != 0 {
		t.Error("the request file outlived the run")
	}
	// A stale request from a dead run never hands the next one off.
	stale := filepath.Join(w.TempDir, "claudinite-request-agent-acme-pack-a-item-4")
	_ = os.WriteFile(stale, []byte("{}"), 0o600)
	res = w.Run(shellTask(t, "true", nil), Work{Item: workitem.Issue{Number: 4}})
	if res.AgentRequested {
		t.Error("a stale request handed the run off")
	}
}

func TestAFailedShellWorkerCarriesItsVerdictAndItsTail(t *testing.T) {
	tk := shellTask(t, `echo 'claudinite-needs-human: action - set the token'; echo boom >&2; exit 3`, nil)
	res := worker(t).Run(tk, Work{Item: workitem.Issue{Number: 4}})
	if res.OK || res.Why != "code-work exited 3" || res.Triage == nil || res.Triage.Kind != "action" || !strings.Contains(res.Detail, "boom") {
		t.Errorf("%+v", res)
	}
}

func TestAnOKShellWorkerMayAskToComeBack(t *testing.T) {
	tk := shellTask(t, `echo 'claudinite-requeue: 2026-09-01T12:00:00Z - not yet'`, nil)
	res := worker(t).Run(tk, Work{Item: workitem.Issue{Number: 4}})
	if !res.OK || res.Requeue == nil || res.Requeue.Until != "2026-09-01T12:00:00.000Z" {
		t.Errorf("%+v", res.Requeue)
	}
	tk = shellTask(t, `echo 'claudinite-requeue: 2026-09-01T12:00:00Z'; exit 1`, nil)
	if res := worker(t).Run(tk, Work{Item: workitem.Issue{Number: 4}}); res.Requeue != nil {
		t.Error("a failed run's requeue was honoured")
	}
}

func TestAnOverrunShellWorkerIsKilledAtItsTimeout(t *testing.T) {
	tk := shellTask(t, `sleep 30`, map[string]any{"code_work_timeout": 1})
	start := time.Now()
	res := worker(t).Run(tk, Work{Item: workitem.Issue{Number: 4}})
	if res.OK || !strings.Contains(res.Why, "timeout") || time.Since(start) > 10*time.Second {
		t.Errorf("%+v", res)
	}
}

func TestAnUnsetDeclaredSecretIsNamedAndNothingRuns(t *testing.T) {
	tk := shellTask(t, `touch ran`, map[string]any{"code_work_required_secrets": []any{"STORE_TOKEN"}})
	res := worker(t).Run(tk, Work{Item: workitem.Issue{Number: 4}})
	if !reflect.DeepEqual(res.MissingSecrets, []string{"STORE_TOKEN"}) {
		t.Errorf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(tk.Dir, "ran")); err == nil {
		t.Error("ran")
	}
}

func TestADeclaredSecretReachesTheWorkerAndNoOtherDoes(t *testing.T) {
	tk := shellTask(t, `echo "$STORE_TOKEN/${OTHER:-none}" > seen`, map[string]any{"code_work_required_secrets": []any{"STORE_TOKEN"}})
	w := worker(t)
	w.Env["CLAUDINITE_SECRETS"] = `{"STORE_TOKEN":"s3","OTHER":"o"}`
	w.Env["OTHER"] = "o"
	if res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}}); !res.OK {
		t.Fatal(res)
	}
	if raw, _ := os.ReadFile(filepath.Join(tk.Dir, "seen")); string(raw) != "s3/none\n" {
		t.Errorf("%q", raw)
	}
}

// A code_worker_mjs worker runs through the embedded runner, and a pull
// request it opened through the SDK is the run's delivery.
func TestAModuleWorkerRunsThroughTheRunnerAndItsOpenedPRIsDelivered(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	dir, err := runner.Unpack(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tk := loopTask("a", map[string]any{"agent_model": "none", "code_worker_mjs": "worker.mjs", "code_work_timeout": 30, "automerge": []any{"doc-changes"}})
	tk.Dir = t.TempDir()
	_ = os.WriteFile(filepath.Join(tk.Dir, "worker.mjs"), []byte(`import { github, log } from '@claudinite/sdk';
export async function worker(params) {
  log('automerge ' + params.automerge);
  const pr = await github.openPr({ title: 't', body: 'b', head: params.target.branch });
  log('opened #' + pr.number);
}
`), 0o644)
	w := worker(t)
	w.Runner = runner.Runner{Dir: dir, Engine: "0.0.0-test"}
	var lines []string
	w.Echo = func(_, line string) { lines = append(lines, line) }
	gh := newSDKWorld(t)
	w.SDK = func(task taskspec.Task, _ workitem.Issue) *SDK {
		return &SDK{Pack: task.Pack, Task: task.ID, Granted: []string{"openPr"}, GitHub: gh, DefaultBranch: "main", Log: func(string) {}}
	}
	res := w.Run(tk, Work{Item: workitem.Issue{Number: 4}, Target: Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/x"}})
	if !res.OK || res.DeliveredPR == 0 || res.Merged {
		t.Fatalf("%+v\n%s", res, strings.Join(lines, "\n"))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "automerge doc-changes") || !strings.Contains(joined, "opened #") {
		t.Error(joined)
	}
}
