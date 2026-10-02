package runner

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func needNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node on PATH")
	}
}

func unpacked(t *testing.T) Runner {
	t.Helper()
	needNode(t)
	dir, err := Unpack(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Runner{Dir: dir, Engine: "61002.9.0"}
}

func taskDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// recorder answers git and two named actions and records every call.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) Methods() []string {
	return []string{"git", "github.createComment", "config", "packs"}
}

func (r *recorder) Handle(method string, args json.RawMessage) (any, error) {
	r.mu.Lock()
	r.calls = append(r.calls, method+" "+string(args))
	r.mu.Unlock()
	switch method {
	case "git":
		return map[string]any{"code": 0, "stdout": "abc123\n", "stderr": ""}, nil
	case "github.createComment":
		return nil, errors.New("createComment is not granted to pack hello")
	case "config":
		return map[string]any{"greeting": "hi"}, nil
	}
	return []any{map[string]any{"id": "hello", "version": "1.4.0"}}, nil
}

func baseEnv(extra ...string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
		"CLAUDINITE_PACK=hello", "CLAUDINITE_TASK=hello-fold", "CLAUDINITE_ITEM=12",
		"CLAUDINITE_TARGET_MODE=fresh", "CLAUDINITE_TARGET_BRANCH=claudinite/hello/hello-fold/2026-10-01-abc",
		"CLAUDINITE_TARGET_PR=", "CLAUDINITE_CONTEXT=a\nb"}
	return append(env, extra...)
}

func TestTheEmbeddedCopyIsPinnedByItsHash(t *testing.T) {
	pin, err := os.ReadFile("js.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(pin)); got != Hash() {
		t.Errorf("js.sha256 says %s; the embedded copy hashes %s — review the change to js/ and update the pin", got, Hash())
	}
	for _, f := range []string{"runner.mjs", "register.mjs", "hooks.mjs", "sdk/index.mjs", "sdk/package.json"} {
		if _, ok := Files()[f]; !ok {
			t.Errorf("%s is not embedded", f)
		}
	}
}

func TestUnpackIsReadOnlyAndReplacesATamperedCopy(t *testing.T) {
	root := t.TempDir()
	dir, err := Unpack(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "runner.mjs"))
	if err != nil || st.Mode().Perm()&0o222 != 0 {
		t.Fatalf("%v %v", st.Mode(), err)
	}
	p := filepath.Join(dir, "sdk", "index.mjs")
	_ = os.Chmod(p, 0o644)
	if err := os.WriteFile(p, []byte("export const params = () => ({ forged: true });"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := Unpack(root)
	if err != nil || again != dir {
		t.Fatalf("%s %v", again, err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(Files()["sdk/index.mjs"]) {
		t.Error("a tampered copy was trusted")
	}
}

const sdkWorker = `import { params, log, git, github, config, packs, commitMessage } from '@claudinite/sdk';
export async function worker(p) {
  log('the sdk answers');
  const same = params() === p;
  const head = await git('rev-parse', 'HEAD');
  let refused = null;
  try { await github.createComment({ issue: 12, body: 'hi' }); } catch (e) { refused = e.message; }
  let unknown = null;
  try { await github.openPr({ title: 't' }); } catch (e) { unknown = e.message; }
  const cfg = await config('hello');
  const list = await packs();
  console.log(JSON.stringify({ same, head: head.stdout.trim(), refused, unknown, cfg, list, ctx: p.context,
    target: p.target, item: p.item, secret: p.secrets, automerge: p.automerge, message: commitMessage('Fold') }));
  process.stdout.write('a stray stdout line\n');
}
`

func TestWorkRunsAWorkerThroughTheSDK(t *testing.T) {
	r := unpacked(t)
	dir := taskDir(t, map[string]string{"worker.mjs": sdkWorker})
	rec := &recorder{}
	var echoed []string
	var mu sync.Mutex
	res := r.Work(Step{Dir: dir, Env: baseEnv("HELLO_TOKEN=s3cret", "NODE_OPTIONS=--require /nonexistent.js"), Timeout: 30 * time.Second, Server: rec,
		Echo: func(stream, line string) { mu.Lock(); echoed = append(echoed, stream+": "+line); mu.Unlock() }},
		"worker.mjs", []string{"HELLO_TOKEN", "UNSET_ONE"}, "hello-generated")
	if !res.OK || res.Err != nil {
		t.Fatalf("%+v\n%s", res, res.Output)
	}
	if !strings.Contains(res.Output, "hello-fold [#12]: the sdk answers") || !strings.Contains(res.Output, "hello/hello-fold: worker done in") {
		t.Errorf("output %s", res.Output)
	}
	var got struct {
		Same      bool              `json:"same"`
		Head      string            `json:"head"`
		Refused   string            `json:"refused"`
		Unknown   string            `json:"unknown"`
		Cfg       map[string]any    `json:"cfg"`
		List      []map[string]any  `json:"list"`
		Ctx       []string          `json:"ctx"`
		Target    map[string]any    `json:"target"`
		Item      map[string]any    `json:"item"`
		Secret    map[string]string `json:"secret"`
		Automerge string            `json:"automerge"`
		Message   string            `json:"message"`
	}
	for _, l := range strings.Split(res.Output, "\n") {
		if strings.HasPrefix(l, `{"same"`) {
			if err := json.Unmarshal([]byte(l), &got); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !got.Same || got.Head != "abc123" || got.Cfg["greeting"] != "hi" || got.List[0]["id"] != "hello" {
		t.Errorf("%+v", got)
	}
	if !strings.Contains(got.Refused, "not granted to pack hello") {
		t.Errorf("refusal %q", got.Refused)
	}
	if !strings.Contains(got.Unknown, "does not answer github.openPr") {
		t.Errorf("unannounced method %q", got.Unknown)
	}
	if strings.Join(got.Ctx, "|") != "a|b" || got.Target["pr"] != nil || got.Target["mode"] != "fresh" || got.Item["number"] != float64(12) {
		t.Errorf("params %+v", got)
	}
	if len(got.Secret) != 1 || got.Secret["HELLO_TOKEN"] != "s3cret" || got.Automerge != "hello-generated" {
		t.Errorf("secrets %+v automerge %q", got.Secret, got.Automerge)
	}
	if got.Message != "Fold\n\nClaudinite-Task: hello/hello-fold\nClaudinite-Automerge-Policy: hello-generated" {
		t.Errorf("message %q", got.Message)
	}
	if len(rec.calls) != 4 || res.Calls["github.openPr"] != 0 || res.Calls["git"] != 1 {
		t.Errorf("calls %v %v", rec.calls, res.Calls)
	}
	joined := strings.Join(echoed, "\n")
	if !strings.Contains(joined, "stderr: hello-fold [#12]: the sdk answers") || !strings.Contains(joined, "stdout: a stray stdout line") {
		t.Errorf("echo %s", joined)
	}
}

func TestTheResolveHookRefusesEveryOtherClaudiniteModule(t *testing.T) {
	r := unpacked(t)
	dir := taskDir(t, map[string]string{
		"worker.mjs":                                "import x from '@claudinite/engine';\nexport const worker = () => x;\n",
		"shadow.mjs":                                "import { params } from 'fake';\nexport const worker = () => params();\n",
		"node_modules/fake/package.json":            `{"name":"fake","type":"module","exports":"./index.mjs"}`,
		"node_modules/fake/index.mjs":               "export * from '@claudinite/sdk';\n",
		"node_modules/@claudinite/sdk/index.mjs":    "export const params = () => ({ forged: true });\n",
		"node_modules/@claudinite/sdk/package.json": `{"name":"@claudinite/sdk","type":"module","exports":"./index.mjs"}`,
	})
	res := r.Work(Step{Dir: dir, Env: baseEnv(), Timeout: 30 * time.Second}, "worker.mjs", nil, "")
	if res.OK || !strings.Contains(res.Output, "@claudinite/engine is refused") || !strings.Contains(res.Output, "hello/hello-fold failed after") {
		t.Errorf("%+v\n%s", res, res.Output)
	}
	res = r.Work(Step{Dir: dir, Env: baseEnv(), Timeout: 30 * time.Second}, "shadow.mjs", nil, "")
	if !res.OK || strings.Contains(res.Output, "forged") {
		t.Errorf("the shipped copy did not win: %+v\n%s", res, res.Output)
	}
}

func TestWorkWritesTheQueueMarkersAndTheRequestFile(t *testing.T) {
	r := unpacked(t)
	dir := taskDir(t, map[string]string{
		"fail.mjs":    "import { fail } from '@claudinite/sdk';\nexport function worker() { fail('action', 'FLEET_GITHUB_TOKEN lacks Actions: write'); }\n",
		"triage.mjs":  "export const worker = () => ({ triage: { kind: 'decision', detail: 'two releases disagree' } });\n",
		"requeue.mjs": "import { requeue } from '@claudinite/sdk';\nexport const worker = () => requeue('2026-10-05T12:00:00Z', 'not yet live');\n",
		"agent.mjs":   "import { requestAgent } from '@claudinite/sdk';\nexport const worker = () => requestAgent({ delivered: { pr: 9 }, reason: { code: 'gate' } });\n",
		"none.mjs":    "export const value = 1;\n",
	})
	run := func(module string, extra ...string) Result {
		return r.Work(Step{Dir: dir, Env: baseEnv(extra...), Timeout: 30 * time.Second}, module, nil, "")
	}
	if res := run("fail.mjs"); res.OK || !strings.Contains(res.Output, "claudinite-needs-human: action - FLEET_GITHUB_TOKEN lacks Actions: write") {
		t.Errorf("%+v %s", res, res.Output)
	}
	if res := run("triage.mjs"); res.OK || res.Code != 1 || !strings.Contains(res.Output, "claudinite-needs-human: decision - two releases disagree") {
		t.Errorf("%+v %s", res, res.Output)
	}
	if res := run("requeue.mjs"); !res.OK || !strings.Contains(res.Output, "claudinite-requeue: 2026-10-05T12:00:00.000Z - not yet live") {
		t.Errorf("%+v %s", res, res.Output)
	}
	req := filepath.Join(t.TempDir(), "request")
	if res := run("agent.mjs", "CLAUDINITE_REQUEST_AGENT="+req); !res.OK {
		t.Errorf("%+v %s", res, res.Output)
	}
	if b, err := os.ReadFile(req); err != nil || string(b) != `{"delivered":{"pr":9},"reason":{"code":"gate"}}` {
		t.Errorf("request file %q %v", b, err)
	}
	if res := run("none.mjs"); res.OK || !strings.Contains(res.Output, "exports no `worker` function") {
		t.Errorf("%+v %s", res, res.Output)
	}
}

func TestTimeoutKillsTheWholeGroup(t *testing.T) {
	r := unpacked(t)
	marker := filepath.Join(t.TempDir(), "survivor")
	dir := taskDir(t, map[string]string{
		"hang.mjs": "import { spawn } from 'node:child_process';\nexport function worker() {\n" +
			"  spawn('sh', ['-c', 'sleep 3; touch " + marker + "'], { stdio: 'ignore' });\n" +
			"  console.log('working');\n  return new Promise(() => setInterval(() => {}, 1000));\n}\n",
	})
	start := time.Now()
	res := r.Work(Step{Dir: dir, Env: baseEnv(), Timeout: 1500 * time.Millisecond}, "hang.mjs", nil, "")
	if res.OK || !res.TimedOut || time.Since(start) > 10*time.Second || !strings.Contains(res.Output, "working") {
		t.Fatalf("%+v %v", res, time.Since(start))
	}
	if !strings.Contains(res.Why("code-work"), "exceeded its timeout") {
		t.Error(res.Why("code-work"))
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("the worker's child outlived the kill")
	}
}

func TestShellRunsACodeWorkCommandAndKillsItsGroup(t *testing.T) {
	needNode(t)
	dir := taskDir(t, map[string]string{"w.sh": "echo in $(basename $PWD) $SECRET_X ${NODE_OPTIONS:-none}\necho oops >&2\nexit 3\n"})
	res := Shell(Step{Dir: dir, Env: []string{"PATH=" + os.Getenv("PATH"), "SECRET_X=x", "NODE_OPTIONS=--inspect"}, Timeout: 10 * time.Second}, "sh w.sh")
	if res.OK || res.Code != 3 || !strings.Contains(res.Output, "in "+filepath.Base(dir)+" x none") || !strings.Contains(res.Output, "oops") {
		t.Fatalf("%+v %q", res, res.Output)
	}
	marker := filepath.Join(t.TempDir(), "survivor")
	res = Shell(Step{Dir: dir, Env: []string{"PATH=" + os.Getenv("PATH")}, Timeout: time.Second}, "(sleep 3; touch "+marker+") & sleep 30")
	if !res.TimedOut || res.OK {
		t.Fatalf("%+v", res)
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("the command's child outlived the kill")
	}
	res = Shell(Step{Dir: filepath.Join(dir, "gone"), Timeout: time.Second}, "true")
	if res.OK || res.Err == nil || !strings.Contains(res.Err.Error(), "does not exist") {
		t.Errorf("%+v", res)
	}
}

const preconditions = `export const terms = {
  'release-out': {
    signals: ['request'],
    takesArg: true,
    holds(signals, { arg, item, config, windowDays, now }) {
      return { holds: signals.request?.state === 'open', reason: arg + ' ' + item.number + ' ' + config.k + ' ' + windowDays + ' ' + now.toISOString(), context: ['one'] };
    },
  },
  'throws': { signals: [], holds() { throw new Error('boom'); } },
};
`

func TestTermsAnswersEveryReferenceInOneCall(t *testing.T) {
	r := unpacked(t)
	dir := taskDir(t, map[string]string{"preconditions.mjs": preconditions, "broken.mjs": "export const terms = {;\n"})
	arg := "v1"
	refs := []Ref{{Name: "release-out", Arg: &arg, Text: "release-out:v1"}, {Name: "throws", Text: "throws"}, {Name: "absent", Text: "absent"}}
	in := TermsInput{Signals: map[string]any{"request": map[string]any{"state": "open"}}, Config: map[string]any{"k": "c"},
		Item: map[string]any{"number": 7}, WindowDays: 1.5, Now: "2026-10-01T00:00:00.000Z"}
	got, err := r.Terms(Step{Dir: dir, Env: baseEnv(), Timeout: 30 * time.Second}, "preconditions.mjs", refs, in)
	if err != nil {
		t.Fatal(err)
	}
	if o := got["release-out:v1"]; !o.Holds || o.Reason != "v1 7 c 1.5 2026-10-01T00:00:00.000Z" || strings.Join(o.Context, ",") != "one" {
		t.Errorf("%+v", o)
	}
	if o := got["throws"]; o.Holds || o.Error != "threw: boom" {
		t.Errorf("%+v", o)
	}
	if o := got["absent"]; !strings.Contains(o.Error, `exports no term "absent"`) {
		t.Errorf("%+v", o)
	}
	if _, err := r.Terms(Step{Dir: dir, Env: baseEnv(), Timeout: 30 * time.Second}, "broken.mjs", refs, in); err == nil || !strings.Contains(err.Error(), "did not load") {
		t.Errorf("%v", err)
	}
}

// The runner speaks the coded checks' framing: the same line bound and the
// same call and answer keys checksdk/pipe.go reads and writes.
func TestThePipeFramingIsTheChecksPipes(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "checksdk", "pipe.go"))
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := os.ReadFile(filepath.Join("..", "..", "checksdk", "checksdk.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`json:"sdk"`, `json:"id"`, `json:"args"`, `json:"result"`, `json:"error"`} {
		if !strings.Contains(string(src), key) {
			t.Errorf("checksdk/pipe.go no longer frames %s", key)
		}
	}
	if !regexp.MustCompile(`const MaxLine = 16 << 20`).Match(sdk) || MaxLine != 16<<20 {
		t.Error("the line bound moved")
	}
	js := string(Files()["runner.mjs"])
	for _, want := range []string{"write({ sdk: method, id, args: args ?? {} })", "typeof msg.id === 'number' && msg.op === undefined && msg.proto === undefined", "msg.result", "msg.error"} {
		if !strings.Contains(js, want) {
			t.Errorf("runner.mjs no longer frames %q", want)
		}
	}
}

func TestAProtocolViolationIsAnError(t *testing.T) {
	r := unpacked(t)
	dir := taskDir(t, map[string]string{"w.mjs": "export const worker = () => {};\n"})
	bad := r
	bad.Dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(bad.Dir, "register.mjs"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad.Dir, "runner.mjs"), []byte("process.stdout.write(JSON.stringify({proto:'claudinite-tasks-v0'})+'\\n');\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := bad.Work(Step{Dir: dir, Env: baseEnv(), Timeout: 10 * time.Second}, "w.mjs", nil, "")
	if res.OK || res.Err == nil || !strings.Contains(res.Err.Error(), "protocol") {
		t.Errorf("%+v", res)
	}
}
