package parity

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The usage face: the whole usage fold, both halves, run over one member
// checkout by each engine, and the two rolling files it pushed compared
// byte for byte. A fixture is testdata/usage/<core>/<name>.json in the
// update face's format, its input a world:
//
//	now          the instant the run reads as now
//	repo         the member's owner/name
//	pack, task, automerge
//	             the trailers the delivery's commits carry
//	packs        the declared packs, [{id, kind}], whose skills are mounted
//	packConfig   the tasks pack's config (actionsMinuteRate)
//	target       {branch, pr}: where the delivery lands
//	main         the base branch's commits, oldest first: {date, files},
//	             a null file a deletion
//	logs         the conversation-logs branch's files; absent, no branch
//	github       the REST API the readers list (usageGitHub)
//	shallow      the depth the member checkout is cloned at; 0 is complete
//	steps        later runs (usageStep), each after the previous run's
//	             branch was landed on main
//
// ClaudinitePacks' worker answers it through testdata/shims/usage.mjs over
// CLAUDINITE_PACKS_TREE, cn through `cndecide usage decide fold --world`.
// The expect is the Node answer: per run, what it printed, the requests it
// made, and the delivery branch's files and commits.

const usageCore = "fold"

// The files a delivery can carry, each read off the delivery branch.
var usageFiles = []string{
	".claudinite/usage/sessions-and-elements.json",
	".claudinite/usage/task-runs-and-costs.json",
	".claudinite/local/usage.GENERATED.json",
	".claudinite/local/tasks-usage.GENERATED.json",
}

type usageWorld struct {
	Now        string              `json:"now"`
	Repo       string              `json:"repo"`
	Pack       string              `json:"pack"`
	Task       string              `json:"task"`
	Automerge  *string             `json:"automerge,omitempty"`
	Packs      []map[string]string `json:"packs,omitempty"`
	PackConfig map[string]any      `json:"packConfig,omitempty"`
	Target     usageTarget         `json:"target"`
	Main       []usageCommit       `json:"main"`
	Logs       map[string]string   `json:"logs,omitempty"`
	GitHub     usageGitHub         `json:"github"`
	Shallow    int                 `json:"shallow,omitempty"`
	Steps      []usageStep         `json:"steps,omitempty"`
}

// usageStep is a later run: its now; unlanded when the previous run's pull
// request is still open, so its branch stays where it was; and mangle, a
// seed that, past zero, rewrites one value deep inside each rolling file
// main holds before the run reads it.
type usageStep struct {
	Now      string `json:"now"`
	Unlanded bool   `json:"unlanded,omitempty"`
	Mangle   uint64 `json:"mangle,omitempty"`
}

type usageTarget struct {
	Branch string `json:"branch"`
	PR     *int   `json:"pr,omitempty"`
}

type usageCommit struct {
	Date  string             `json:"date"`
	Files map[string]*string `json:"files"`
}

// usageGitHub is the REST API as the readers list it: each listing
// filtered, ordered and paged the way GitHub answers it, a missing thing a
// 404, and Fail answering a path with that prefix by its status or, for
// "unreachable", with a dropped connection.
type usageGitHub struct {
	Runs      map[string][]map[string]any `json:"runs,omitempty"`
	Jobs      map[string][]map[string]any `json:"jobs,omitempty"`
	Logs      map[string]string           `json:"logs,omitempty"`
	Issues    []map[string]any            `json:"issues,omitempty"`
	Timelines map[string][]any            `json:"timelines,omitempty"`
	Events    map[string][]any            `json:"events,omitempty"`
	Pulls     []map[string]any            `json:"pulls,omitempty"`
	Releases  []map[string]any            `json:"releases,omitempty"`
	Fail      map[string]string           `json:"fail,omitempty"`
}

var workflowFiles = map[string]string{"claudinite-scheduler.yml": "scheduler", "claudinite-executor.yml": "executor"}

// serve answers one engine's run, recording each path it asked for.
func (g usageGitHub) serve(t *testing.T) (*httptest.Server, func() []string) {
	var mu sync.Mutex
	var asked []string
	routes := []struct {
		re     *regexp.Regexp
		answer func(m []string, q url.Values) (any, bool)
	}{
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/actions/workflows/([^/]+)/runs$`), func(m []string, q url.Values) (any, bool) {
			from := strings.TrimPrefix(q.Get("created"), ">=")
			var kept []map[string]any
			for _, r := range g.Runs[workflowFiles[m[1]]] {
				if r == nil {
					kept = append(kept, r)
					continue
				}
				at, _ := r["created_at"].(string)
				if len(at) >= 10 && at[:10] >= from {
					kept = append(kept, r)
				}
			}
			return map[string]any{"total_count": len(kept), "workflow_runs": page(kept, q)}, true
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/actions/runs/(\d+)/jobs$`), func(m []string, _ url.Values) (any, bool) {
			jobs, ok := g.Jobs[m[1]]
			return map[string]any{"total_count": len(jobs), "jobs": orEmpty(jobs)}, ok
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/actions/jobs/(\d+)/logs$`), func(m []string, _ url.Values) (any, bool) {
			text, ok := g.Logs[m[1]]
			return text, ok
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues$`), func(_ []string, q url.Values) (any, bool) {
			since := q.Get("since")
			var kept []map[string]any
			for _, i := range byUpdated(g.Issues) {
				if q.Get("state") == "closed" && i["state"] != "closed" {
					continue
				}
				if at, _ := i["updated_at"].(string); at >= since {
					kept = append(kept, i)
				}
			}
			return page(kept, q), true
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues/(\d+)/timeline$`), func(m []string, q url.Values) (any, bool) {
			events, ok := g.Timelines[m[1]]
			return page(events, q), ok
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues/(\d+)/events$`), func(m []string, q url.Values) (any, bool) {
			events, ok := g.Events[m[1]]
			return page(events, q), ok
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues/(\d+)$`), func(m []string, _ url.Values) (any, bool) {
			for _, i := range g.Issues {
				if fmt.Sprint(i["number"]) == m[1] {
					return i, true
				}
			}
			return nil, false
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/pulls$`), func(_ []string, q url.Values) (any, bool) {
			return page(byUpdated(g.Pulls), q), true
		}},
		{regexp.MustCompile(`^/repos/[^/]+/[^/]+/releases$`), func(_ []string, q url.Values) (any, bool) {
			return page(g.Releases, q), g.Releases != nil
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RequestURI())
		mu.Unlock()
		for prefix, how := range g.Fail {
			if !strings.HasPrefix(r.URL.RequestURI(), prefix) {
				continue
			}
			if how == "unreachable" {
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = conn.Close()
				}
				return
			}
			code, _ := strconv.Atoi(how)
			http.Error(w, `{"message":"failed"}`, code)
			return
		}
		for _, route := range routes {
			m := route.re.FindStringSubmatch(r.URL.Path)
			if m == nil {
				continue
			}
			v, ok := route.answer(m, r.URL.Query())
			if !ok {
				break
			}
			if s, isText := v.(string); isText {
				_, _ = w.Write([]byte(s))
				return
			}
			w.Header().Set("content-type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
			return
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, asked...)
	}
}

func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

// page is one page of a listing, by its per_page and page parameters.
func page[T any](list []T, q url.Values) []T {
	per, err := strconv.Atoi(q.Get("per_page"))
	if err != nil || per <= 0 {
		per = 30
	}
	n, err := strconv.Atoi(q.Get("page"))
	if err != nil || n <= 0 {
		n = 1
	}
	from := (n - 1) * per
	if from >= len(list) {
		return []T{}
	}
	return list[from:min(len(list), from+per)]
}

func byUpdated(list []map[string]any) []map[string]any {
	out := append([]map[string]any{}, list...)
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := out[i]["updated_at"].(string)
		b, _ := out[j]["updated_at"].(string)
		return a > b
	})
	return out
}

// usageMember is one engine's copy of a world's repository: a bare
// origin, the writer that lands commits on it, and the member checkout
// the fold runs in.
type usageMember struct{ origin, writer, root string }

func usageGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GIT_AUTHOR_NAME=acme", "GIT_AUTHOR_EMAIL=acme@example.com",
		"GIT_COMMITTER_NAME=acme", "GIT_COMMITTER_EMAIL=acme@example.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}, env...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeTree(t *testing.T, dir string, files map[string]*string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if body == nil {
			_ = os.Remove(full)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(*body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newUsageMember(t *testing.T, w usageWorld) usageMember {
	t.Helper()
	dir := t.TempDir()
	m := usageMember{origin: filepath.Join(dir, "origin.git"), writer: filepath.Join(dir, "writer"), root: filepath.Join(dir, "member")}
	usageGit(t, dir, nil, "init", "-q", "--bare", "-b", "main", m.origin)
	usageGit(t, dir, nil, "init", "-q", "-b", "main", m.writer)
	usageGit(t, m.writer, nil, "remote", "add", "origin", m.origin)
	for i, c := range w.Main {
		writeTree(t, m.writer, c.Files)
		dated := []string{"GIT_AUTHOR_DATE=" + c.Date, "GIT_COMMITTER_DATE=" + c.Date}
		usageGit(t, m.writer, nil, "add", "-A")
		usageGit(t, m.writer, dated, "commit", "-q", "--allow-empty", "-m", "commit "+strconv.Itoa(i+1))
	}
	usageGit(t, m.writer, nil, "push", "-q", "origin", "main")
	if w.Logs != nil {
		usageGit(t, m.writer, nil, "checkout", "-q", "--orphan", "conversation-logs")
		usageGit(t, m.writer, nil, "rm", "-rq", "--cached", ".")
		usageGit(t, m.writer, nil, "clean", "-qfdx")
		files := map[string]*string{}
		for name, body := range w.Logs {
			files[name] = &body
		}
		writeTree(t, m.writer, files)
		usageGit(t, m.writer, nil, "add", "-A")
		usageGit(t, m.writer, nil, "commit", "-q", "--allow-empty", "-m", "logs")
		usageGit(t, m.writer, nil, "push", "-q", "origin", "conversation-logs")
		usageGit(t, m.writer, nil, "checkout", "-q", "-f", "main")
	}
	clone := []string{"clone", "-q", "-b", "main"}
	if w.Shallow > 0 {
		clone = append(clone, "--depth", strconv.Itoa(w.Shallow), "file://"+m.origin)
	} else {
		clone = append(clone, m.origin)
	}
	usageGit(t, dir, nil, append(clone, m.root)...)
	return m
}

// The rolling files a step's mangle rewrites, at their homes.
var mangled = []string{".claudinite/usage/sessions-and-elements.json", ".claudinite/usage/task-runs-and-costs.json"}

// mangle replaces one value inside each rolling file on main, chosen by
// seed, with a value of another shape, and commits that onto main.
func (m usageMember) mangle(t *testing.T, seed uint64) {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, 1))
	usageGit(t, m.writer, nil, "fetch", "-q", "origin", "main")
	usageGit(t, m.writer, nil, "reset", "-q", "--hard", "FETCH_HEAD")
	changed := map[string]*string{}
	for _, p := range mangled {
		raw, err := os.ReadFile(filepath.Join(m.writer, p))
		if err != nil {
			continue
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		v = mangleValue(r, v, 0)
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		body := string(out) + "\n"
		changed[p] = &body
	}
	if len(changed) == 0 {
		return
	}
	writeTree(t, m.writer, changed)
	usageGit(t, m.writer, nil, "add", "-A")
	usageGit(t, m.writer, nil, "commit", "-q", "--allow-empty", "-m", "mangle")
	usageGit(t, m.writer, nil, "push", "-q", "origin", "HEAD:main")
	usageGit(t, m.root, nil, "fetch", "-q", "origin")
	usageGit(t, m.root, nil, "reset", "-q", "--hard", "origin/main")
}

// mangleValue walks down a random path and swaps what it finds there for
// another shape, or drops it.
func mangleValue(r *rand.Rand, v any, depth int) any {
	shapes := []any{nil, 7.0, -1.0, "x", "2026-08-20", true, []any{}, []any{1.0, "a"}, map[string]any{}, map[string]any{"a": 1.0}}
	switch x := v.(type) {
	case map[string]any:
		if len(x) > 0 && (depth < 2 || r.IntN(4) > 0) {
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			k := keys[r.IntN(len(keys))]
			if r.IntN(8) == 0 {
				delete(x, k)
				return x
			}
			x[k] = mangleValue(r, x[k], depth+1)
			return x
		}
	case []any:
		if len(x) > 0 && r.IntN(3) > 0 {
			i := r.IntN(len(x))
			x[i] = mangleValue(r, x[i], depth+1)
			return x
		}
	}
	return shapes[r.IntN(len(shapes))]
}

// read is what one run left on the delivery branch: the files a delivery
// can carry, each as its bytes or null, and its commits' messages, oldest
// first, past main.
func (m usageMember) read(t *testing.T, branch string) map[string]any {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", m.origin, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err := cmd.Run(); err != nil || branch == "" {
		return map[string]any{"exists": false}
	}
	files := map[string]any{}
	for _, p := range usageFiles {
		out, err := exec.Command("git", "--git-dir", m.origin, "show", branch+":"+p).Output()
		if err != nil {
			files[p] = nil
			continue
		}
		files[p] = string(out)
	}
	log := usageGit(t, m.origin, nil, "log", "--reverse", "--format=%B%x00", "main.."+branch)
	var messages []any
	for _, msg := range strings.Split(log, "\x00") {
		if msg = strings.TrimSpace(msg); msg != "" {
			messages = append(messages, msg)
		}
	}
	return map[string]any{"exists": true, "files": files, "commits": messages}
}

// land fast-forwards main to the delivery branch, as a merge of the fold's
// pull request would, and brings the checkout up to it.
func (m usageMember) land(t *testing.T, branch string) {
	t.Helper()
	if branch == "main" || exec.Command("git", "--git-dir", m.origin, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() != nil {
		return
	}
	usageGit(t, m.origin, nil, "update-ref", "refs/heads/main", "refs/heads/"+branch)
	usageGit(t, m.origin, nil, "update-ref", "-d", "refs/heads/"+branch)
	usageGit(t, m.root, nil, "fetch", "-q", "origin")
	usageGit(t, m.root, nil, "reset", "-q", "--hard", "origin/main")
}

var (
	halfFailed = regexp.MustCompile(`^(the \w+ half failed - its file is unchanged this run: )`)
	// transportFailed is how net/http says a server dropped the
	// connection, where fetch says "fetch failed".
	transportFailed = regexp.MustCompile(`Get "[^"]*": [^;]*`)
)

// askUsage runs one engine over the world's runs and answers what each
// left behind.
func askUsage(t *testing.T, side string, w usageWorld, decide string) []any {
	t.Helper()
	m := newUsageMember(t, w)
	var runs []any
	for i, step := range steps(w) {
		now := step.Now
		if i > 0 {
			if !step.Unlanded {
				m.land(t, w.Target.Branch)
			}
			if step.Mangle != 0 {
				m.mangle(t, step.Mangle)
			}
		}
		srv, asked := w.GitHub.serve(t)
		in := map[string]any{"root": m.root, "repo": w.Repo, "now": now, "pack": w.Pack, "task": w.Task,
			"packs": orEmpty(w.Packs), "packConfig": w.PackConfig, "target": w.Target, "api": srv.URL}
		if w.Automerge != nil {
			in["automerge"] = *w.Automerge
		}
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr string
		var code int
		switch side {
		case "node":
			shim, aerr := filepath.Abs(filepath.Join("testdata", "shims", "usage.mjs"))
			if aerr != nil {
				t.Fatal(aerr)
			}
			stdout, stderr, code, err = run(m.root, nil, string(raw), "node", shim)
		default:
			file := filepath.Join(t.TempDir(), "world.json")
			if werr := os.WriteFile(file, raw, 0o644); werr != nil {
				t.Fatal(werr)
			}
			var env []string
			if dir := os.Getenv(usageCoverEnv); dir != "" {
				env = append(env, "GOCOVERDIR="+dir)
			}
			stdout, stderr, code, err = run(m.root, env, "", decide, "usage", "decide", usageCore, "--world", file)
		}
		if err != nil || code != 0 {
			t.Fatalf("%s run %d exited %d: %v\n%s", side, i+1, code, err, stderr)
		}
		var said map[string]any
		if err := json.Unmarshal([]byte(stdout), &said); err != nil {
			t.Fatalf("%s printed no JSON answer: %v\n%s\n%s", side, err, stdout, stderr)
		}
		if logs, ok := said["logs"].([]any); ok {
			for j, l := range logs {
				if s, isString := l.(string); isString {
					if mm := halfFailed.FindStringSubmatch(s); mm != nil {
						logs[j] = mm[1] + "…"
					}
				}
			}
		}
		if e, ok := said["error"].(string); ok {
			said["error"] = transportFailed.ReplaceAllString(e, "fetch failed")
		}
		said["requests"] = orEmpty(asked())
		runs = append(runs, map[string]any{"said": said, "branch": m.read(t, w.Target.Branch)})
	}
	return runs
}

// usageCoverEnv names a directory the cn side writes its coverage of the
// fold's package into, for `go tool covdata`: the measure of which of
// its branches the fixtures and the fuzz reach.
const usageCoverEnv = "CLAUDINITE_PARITY_USAGE_COVERDIR"

// usageDecideBinary is cndecide, built to record its coverage of the
// fold's package when usageCoverEnv asks for it.
func usageDecideBinary(t *testing.T) string {
	t.Helper()
	if os.Getenv(usageCoverEnv) == "" {
		return decideBinary(t)
	}
	bin := filepath.Join(t.TempDir(), "cndecide")
	cmd := exec.Command("go", "build", "-cover", "-coverpkg=github.com/missingbulb/ClaudiniteEngine/cn/tasks/usage,github.com/missingbulb/ClaudiniteEngine/rewrite-temp/cndecide", "-o", bin, "../cndecide")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build cndecide: %v\n%s", err, out)
	}
	return bin
}

// usageSides are the engines the face runs: the Node worker when
// CLAUDINITE_PACKS_TREE names a ClaudinitePacks checkout, cn always, unless
// CLAUDINITE_PARITY_ENGINE narrows them.
func usageSides(t *testing.T) []string {
	sel := os.Getenv(engineEnv)
	var out []string
	if (sel == "" || sel == "both" || sel == "node") && os.Getenv(PacksTreeEnv) != "" {
		out = append(out, "node")
	}
	if sel == "" || sel == "both" || sel == "cn" {
		out = append(out, "cn")
	}
	if len(out) == 0 {
		t.Skipf("%s=%q and no %s: no side to run", engineEnv, sel, PacksTreeEnv)
	}
	return out
}

func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// sameBytes fails on the first rolling file whose bytes differ between
// the two answers, naming the first line they part at.
func sameBytes(t *testing.T, who string, got, want []any) {
	t.Helper()
	for i := range want {
		if i >= len(got) {
			return
		}
		gb, _ := got[i].(map[string]any)["branch"].(map[string]any)
		wb, _ := want[i].(map[string]any)["branch"].(map[string]any)
		gf, _ := gb["files"].(map[string]any)
		wf, _ := wb["files"].(map[string]any)
		for _, p := range usageFiles {
			g, _ := gf[p].(string)
			w, _ := wf[p].(string)
			if g == w {
				continue
			}
			gl, wl := strings.Split(g, "\n"), strings.Split(w, "\n")
			n := 0
			for n < len(gl) && n < len(wl) && gl[n] == wl[n] {
				n++
			}
			at := func(l []string) string {
				if n < len(l) {
					return l[n]
				}
				return "<end of file>"
			}
			t.Errorf("run %d: %s writes %s differently from line %d:\n  %s: %s\n  want: %s", i+1, who, p, n+1, who, at(gl), at(wl))
		}
	}
}

func readUsageWorld(t *testing.T, raw json.RawMessage) usageWorld {
	t.Helper()
	var w usageWorld
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestParityUsage(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "usage"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no usage fixtures under testdata/usage")
	}
	sides := usageSides(t)
	decide := usageDecideBinary(t)
	recording := os.Getenv(recordEnv) == "1"
	for _, f := range fixtures {
		t.Run(f.Core+"/"+f.Name, func(t *testing.T) {
			w := readUsageWorld(t, f.Input)
			for _, side := range sides {
				got := asJSON(t, askUsage(t, side, w, decide)).([]any)
				if recording && side == "node" {
					if err := recordUpdate(f, got); err != nil {
						t.Fatal(err)
					}
					f.Expect, _ = json.Marshal(got)
					continue
				}
				if len(f.Expect) == 0 {
					t.Fatalf("%s has no expect; record it against ClaudinitePacks first (%s=1)", f.Path, recordEnv)
				}
				wantRaw := f.Expect
				if side == "cn" && f.Divergence != "" {
					wantRaw = f.Cn
				}
				var want []any
				if err := json.Unmarshal(wantRaw, &want); err != nil {
					t.Fatal(err)
				}
				sameBytes(t, side, got, want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s disagrees:\n%s", side, strings.Join(jsonDiff("$", any(got), any(want), 30), "\n"))
				}
			}
		})
	}
}

// The parity README counts the usage face's divergences.
func TestUsageDivergencesAreCounted(t *testing.T) {
	fixtures, err := LoadUpdateFixtures(filepath.Join("testdata", "usage"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range fixtures {
		if f.Divergence != "" {
			n++
		}
	}
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^Usage face divergences: (\d+) of (\d+) fixtures\.$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("parity/README.md has no line `Usage face divergences: <n> of <total> fixtures.`")
	}
	if want := fmt.Sprintf("%d of %d", n, len(fixtures)); m[1]+" of "+m[2] != want {
		t.Errorf("parity/README.md counts %s of %s usage divergences, the fixtures mark %s", m[1], m[2], want)
	}
}
