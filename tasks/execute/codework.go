package execute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/runner"
)

// The two bags a workflow hands the executor: every repository secret and
// every repository variable, each one JSON object.
const (
	SecretsBagEnv = "CLAUDINITE_SECRETS"
	VarsBagEnv    = "CLAUDINITE_VARS"
)

func parseBag(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) != nil || obj == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range obj {
		switch x := v.(type) {
		case string:
			out[k] = x
		case nil:
		default:
			b, _ := json.Marshal(x)
			out[k] = string(b)
		}
	}
	return out
}

// Secret is one secret as a code-work would receive it: the job's
// CLAUDINITE_SECRETS bag first, then the env.
func Secret(name string, env map[string]string) (string, bool) {
	return secretValue(name, env, parseBag(env[SecretsBagEnv]))
}

func secretValue(name string, env, bag map[string]string) (string, bool) {
	if name == SecretsBagEnv {
		return "", false
	}
	if v, ok := bag[name]; ok {
		return v, true
	}
	v, ok := env[name]
	return v, ok
}

// MissingSecrets are the declared secrets this job carries neither in its
// bag nor its environment; a set-but-empty one is the repo's own choice.
func MissingSecrets(names []string, env map[string]string) []string {
	bag := parseBag(env[SecretsBagEnv])
	out := []string{}
	for _, n := range names {
		if _, ok := secretValue(n, env, bag); !ok {
			out = append(out, n)
		}
	}
	return out
}

// TaskEnv is the environment a task's work step runs under: the job's,
// minus every secret in the bag, both bags themselves and every name in
// withheld — the secrets the job carries for other tasks and for the
// routine endpoints — plus every repository variable the job does not
// already set, plus the secrets the task declared. Secrets are selected;
// variables are not. GITHUB_TOKEN is not withheld: the job token is the
// surface every work step writes through.
func TaskEnv(names, withheld []string, env map[string]string) map[string]string {
	secrets := parseBag(env[SecretsBagEnv])
	hold := map[string]bool{}
	for _, n := range withheld {
		hold[n] = true
	}
	out := map[string]string{}
	for k, v := range env {
		if _, inBag := secrets[k]; inBag || hold[k] || k == SecretsBagEnv || k == VarsBagEnv {
			continue
		}
		out[k] = v
	}
	for k, v := range parseBag(env[VarsBagEnv]) {
		if k == VarsBagEnv || hold[k] {
			continue
		}
		if _, set := env[k]; !set {
			out[k] = v
		}
	}
	for _, n := range names {
		if v, ok := secretValue(n, env, secrets); ok {
			out[n] = v
		}
	}
	return out
}

// WithheldSecrets are the names a job carries that no work step inherits
// unless its task declares them: every discovered task's
// code_work_required_secrets, and every invocation endpoint's
// tokenSecret, the default endpoint's default included.
func WithheldSecrets(tasks []taskspec.Task, endpoints map[string]any) []string {
	decls := make([]taskspec.Decl, 0, len(tasks))
	for _, t := range tasks {
		decls = append(decls, t.Decl)
	}
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range taskspec.SecretNames(decls) {
		add(n)
	}
	add(DefaultTokenSecret)
	for _, v := range endpoints {
		if entry, ok := v.(map[string]any); ok {
			if s, _ := entry["tokenSecret"].(string); s != "" {
				add(s)
			}
		}
	}
	sort.Strings(out)
	return out
}

// CodeWorkPlace is where every work step of one run happens.
type CodeWorkPlace struct {
	Root, Repo, DefaultBranch string
	// EngineDir holds the running engine, first on code-work's PATH, so
	// a code_work naming cn reaches the engine that runs it.
	EngineDir string
}

// CodeWorkEnv is the whole of the CLAUDINITE_* variables code-work is
// handed, by the names taskspec.CodeWorkEnvVars holds.
func CodeWorkEnv(p CodeWorkPlace, t taskspec.Task, item workitem.Issue, context []string, requestPath string, target Target) map[string]string {
	env := map[string]string{
		"CLAUDINITE_REPO_ROOT":      p.Root,
		"CLAUDINITE_REPO":           p.Repo,
		"CLAUDINITE_DEFAULT_BRANCH": p.DefaultBranch,
		"CLAUDINITE_ITEM":           strconv.Itoa(item.Number),
		"CLAUDINITE_PACK":           t.Pack,
		"CLAUDINITE_TASK":           t.ID,
		"CLAUDINITE_CONTEXT":        strings.Join(context, "\n"),
		"CLAUDINITE_REQUEST_AGENT":  requestPath,
	}
	for _, kv := range target.Env() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return env
}

var (
	triageRE  = regexp.MustCompile(`(?m)^claudinite-needs-human:[ \t]*([a-z]+)\b[ \t]*(.*)$`)
	requeueRE = regexp.MustCompile(`(?m)^claudinite-requeue:[ \t]*(\S+)[ \t]*(.*)$`)
	leadRE    = regexp.MustCompile(`^[—\-:\s]+`)
)

func lastMatch(re *regexp.Regexp, text string) []string {
	all := re.FindAllStringSubmatch(text, -1)
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

// ReadTriage is a failed worker's own verdict, the last marker winning.
func ReadTriage(output string) *Triage {
	m := lastMatch(triageRE, output)
	if m == nil {
		return nil
	}
	return &Triage{Kind: m[1], Detail: strings.TrimSpace(leadRE.ReplaceAllString(strings.TrimRight(m[2], "\r"), ""))}
}

// instantForms are the spellings a requeue instant is read in.
var instantForms = []string{time.RFC3339Nano, "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"}

// ReadRequeue is a worker's come-back-later ask, the last marker winning;
// an instant that does not read leaves Until "".
func ReadRequeue(output string) *Requeue {
	m := lastMatch(requeueRE, output)
	if m == nil {
		return nil
	}
	r := &Requeue{Reason: strings.TrimSpace(leadRE.ReplaceAllString(strings.TrimRight(m[2], "\r"), ""))}
	for _, f := range instantForms {
		if at, err := time.Parse(f, m[1]); err == nil {
			r.Until = at.UTC().Format("2006-01-02T15:04:05.000Z")
			break
		}
	}
	return r
}

type agentRequest struct {
	Branch string
	PR     int
	Merged bool
	Issue  int
	Reason string
}

func readAgentRequest(path string) *agentRequest {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var payload struct {
		Delivered *struct {
			Branch string `json:"branch"`
			PR     int    `json:"pr"`
			Merged bool   `json:"merged"`
			Issue  int    `json:"issue"`
		} `json:"delivered"`
		Reason *struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		} `json:"reason"`
	}
	out := &agentRequest{}
	if json.Unmarshal(raw, &payload) != nil {
		return out
	}
	if d := payload.Delivered; d != nil {
		out.Branch, out.PR, out.Merged, out.Issue = d.Branch, d.PR, d.Merged, d.Issue
	}
	if r := payload.Reason; r != nil {
		out.Reason = r.Detail
		if out.Reason == "" {
			out.Reason = r.Code
		}
	}
	return out
}

// CodeWorker runs one task's code-work: a declared code_work command
// through the shell, or a code_worker_mjs module through the embedded
// runner, which answers the SDK the task's pack was granted.
type CodeWorker struct {
	Runner runner.Runner
	Place  CodeWorkPlace
	Env    map[string]string
	// Withheld are the job's secrets no work step inherits undeclared
	// (WithheldSecrets).
	Withheld []string
	TempDir  string
	Echo     func(stream, line string)
	Log      func(string)
	// SDK is the server a module worker's calls reach; nil serves none.
	SDK func(t taskspec.Task, item workitem.Issue) *SDK
}

// requestPath is where a worker writes its ask for the agent.
func (c CodeWorker) requestPath(t taskspec.Task, item int) string {
	dir := c.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "claudinite-request-agent-"+t.Pack+"-"+t.ID+"-item-"+strconv.Itoa(item))
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func tail(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Run is one code-work run. Re-entrancy is the requirement: a reclaimed
// item runs again over its own half-done work.
func (c CodeWorker) Run(t taskspec.Task, w Work) CodeWorkResult {
	secrets := t.Decl.Strings("code_work_required_secrets")
	if missing := MissingSecrets(secrets, c.Env); len(missing) > 0 {
		return CodeWorkResult{OK: true, MissingSecrets: missing}
	}
	request := c.requestPath(t, w.Item.Number)
	_ = os.Remove(request)
	defer func() { _ = os.Remove(request) }()

	env := TaskEnv(secrets, c.Withheld, c.Env)
	for k, v := range CodeWorkEnv(c.Place, t, w.Item, w.Context, request, w.Target) {
		env[k] = v
	}
	if c.Place.EngineDir != "" {
		if env["PATH"] == "" {
			env["PATH"] = c.Place.EngineDir
		} else {
			env["PATH"] = c.Place.EngineDir + string(os.PathListSeparator) + env["PATH"]
		}
	}
	timeout, _ := t.Decl.Num("code_work_timeout")
	step := runner.Step{Dir: t.Dir, Env: envList(env), Timeout: time.Duration(timeout * float64(time.Second)), Echo: c.Echo}
	var sdk *SDK
	if c.SDK != nil {
		sdk = c.SDK(t, w.Item)
		if sdk != nil {
			step.Server = sdk
		}
	}
	c.Log("::group::code_work " + t.Path() + " [#" + strconv.Itoa(w.Item.Number) + "]")
	var res runner.Result
	if module, ok := t.Decl.Str("code_worker_mjs"); ok {
		res = c.Runner.Work(step, module, secrets, mergepolicy.Expression(t.Decl["automerge"]))
	} else {
		command, _ := t.Decl.Str("code_work")
		res = runner.Shell(step, command)
	}
	c.Log("::endgroup::")

	if !res.OK {
		detail := tail(res.Output, 5)
		if res.Err != nil && !res.TimedOut {
			detail = strings.TrimSpace(detail + "\n" + res.Err.Error())
		}
		if detail != "" {
			c.Log(detail)
		}
		return CodeWorkResult{Why: res.Why("code-work"), Detail: detail, Triage: ReadTriage(res.Output)}
	}
	out := CodeWorkResult{OK: true, Requeue: ReadRequeue(res.Output)}
	if req := readAgentRequest(request); req != nil {
		out.AgentRequested = true
		out.DeliveredPR, out.Merged, out.Branch, out.Issue, out.Reason = req.PR, req.Merged, req.Branch, req.Issue, req.Reason
	}
	if sdk != nil && out.DeliveredPR == 0 && len(sdk.Opened) > 0 {
		out.DeliveredPR = sdk.Opened[len(sdk.Opened)-1].Number
		if out.Branch == "" {
			out.Branch = sdk.Opened[len(sdk.Opened)-1].HeadRef
		}
	}
	return out
}
