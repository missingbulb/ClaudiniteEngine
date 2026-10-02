// Package checksdk is the public Go check SDK: the one package a pack's Go
// checks import, besides the standard library. A pack's checks/ folder is
// package checks; each check registers itself from an init function:
//
//	func init() {
//		checksdk.Register(checksdk.Check{ID: "hello-check", Tags: []string{"work", "world"}, Run: run})
//	}
//
// The engine compiles every declared pack's checks into one checks binary
// whose main calls Main, and talks to it over its stdin and stdout, one
// JSON object per line: first a handshake {"proto": Proto, "engine": ...,
// "methods": [...]} answered with the same proto, then any number of
// requests, one at a time:
//
//	{"op":"list"}                                   -> {"checks":[{"check":"<pack>/<id>","tags":[...],"judge":true,"on_fail":"block","since":"YYYY-MM-DD"}]}
//	{"op":"run","tags":[...],"pack":"","repo":DIR}  -> {"findings":[{"check","class","path","line","sentence","why","fix","on_fail","since"}],"errors":[...]}
//	{"op":"judge","event":E,"call":{...},"repo":DIR} -> the same answer as run
//
// While a request is being answered a check may call back to the engine
// for what the engine already holds: the child writes
// {"sdk":METHOD,"id":N,"args":{...}} and reads {"id":N,"result":...} or
// {"id":N,"error":"..."}, one call in flight at a time. The handshake's
// methods are the calls the engine answers; a Repo method whose call the
// engine did not announce panics, which fails that check alone.
//
// A run selects every check with a Run whose tags include each requested
// tag, from one pack when pack is set. A judge op selects every check with
// a Judge tagged with its event (pre-tool-use, post-tool-use or
// user-prompt-submit) and hands it the call: the tool's name, its input
// and, after it ran, its response, as Claude Code sent them, or the
// prompt. A check that panics, or spends more than CheckDeadline of its
// own work (time waiting on engine answers does not count, up to
// MaxEngineWaits deadlines of it), is reported
// in errors and the others still run. Lines are capped at 16 MiB.
//
// The public contract a check reads the repository through is Repo:
//
//	Root, Path(rel), Exists(rel), ReadFile(rel), Read(rel)    the working tree, read from disk
//	Files(), Tracked(), AllFiles(), Untracked()               the walk: scanned, tracked, scanned with vendored, untracked
//	ChangedFiles(), Deleted()                                 the change against the merge base
//	Branch(), BaseRef(), MergeBase(), OnDefaultBranch()       where the change sits
//	ReadBase(rel), ListBase()                                 the tree at the merge base
//	AddedLines(files), RemovedLines(files)                    the change's lines (nil files: every changed file)
//	BranchCommits(), CommitMessages(), IntroducedMerges()     the change's commits
//	GrepTracked(needle)                                       a fixed-string search over tracked files
//	PackConfig(id), ChecksConfig()                            the member's settings
//	Parsed(rel)                                               a JSON, YAML or TOML document, parsed
//	Session()                                                 the session transcript: OwnerTurns, ReplyClasses, ToolCalls, SkillLoads
//
// Every engine call is memoised for the run. NewRepo builds a Repo over
// any Handler, and Fake is one for a pack's own unit tests.
//
// Its import path is claudinite.com/checksdk, which the engine resolves to
// the copy it unpacks into its cache, never the network.
package checksdk

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Proto is the pipe's protocol version.
const Proto = "claudinite-checks-v1"

// MaxLine bounds one message on the pipe.
const MaxLine = 16 << 20

// EngineFloor is the first engine that answers SDK calls. Its day part is
// never after today's, so it names a version that can exist: the first
// release candidate, cut on or after 2026-10-01 (day 61001), satisfies it
// by construction, and release/version.sh refuses any version below it,
// reading it from engine_floor.txt beside this file, which a test holds
// equal to this constant.
const EngineFloor = "61001.1.0"

// CheckDeadline is how long one check may work, engine answers excluded; Main reads
// CLAUDINITE_CHECK_DEADLINE_MS over it.
var CheckDeadline = 10 * time.Second

// MaxEngineWaits is how many deadlines' worth of engine answers one check
// may wait on before it is stopped as if its own work had run out.
const MaxEngineWaits = 10

// Class is how much a finding matters.
type Class string

const (
	// ClassFinding fails the run that selected the check: it blocks a
	// stop, fails cn check world.
	ClassFinding Class = "finding"
	// ClassAdvisory is reported and fails nothing.
	ClassAdvisory Class = "advisory"
)

// Finding is one problem a check reports, about Path (relative to the
// repo root) at Line (0 for the whole path): Sentence is what is wrong,
// Fix how to fix it, Why why it matters (the check's Why when empty). An
// empty Class takes the check's OnFail.
type Finding struct {
	Class    Class  `json:"class"`
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Sentence string `json:"sentence"`
	Why      string `json:"why,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

// Call is one call a hook judges: a tool call about to run
// (pre-tool-use) or just run (post-tool-use, with its Response), or the
// person's prompt (user-prompt-submit).
type Call struct {
	Tool     string          `json:"tool,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Prompt   string          `json:"prompt,omitempty"`
}

// HookEvents are the tags a judge is selected by.
var HookEvents = []string{"pre-tool-use", "post-tool-use", "user-prompt-submit"}

// Check is one coded check: a Run over the repo, a Judge of a hook's
// call, or both.
type Check struct {
	// ID is unique within its pack: lowercase letters, digits and dashes.
	ID string
	// Tags select the check: its scope (work, world or a hook event) and
	// any the pack adds. A check with a Judge carries the hook events it
	// judges, and a check carrying one has a Judge.
	Tags []string
	// OnFail is "block" (the default when empty) or "advise".
	OnFail string
	// Since is the date the check was added, YYYY-MM-DD: a blocking
	// check only advises for its first 14 days.
	Since string
	// Why is why a finding matters; Doc names the page that says more.
	Why, Doc string
	Run      func(Repo) []Finding
	// Judge judges one call. A finding of class finding blocks a call
	// about to run; elsewhere every finding is passed on as context.
	Judge func(Repo, Call) []Finding
}

func (c Check) hookEvent() bool {
	for _, t := range c.Tags {
		for _, e := range HookEvents {
			if t == e {
				return true
			}
		}
	}
	return false
}

func (c Check) onFail() string {
	if c.OnFail == "" {
		return "block"
	}
	return c.OnFail
}

type registered struct {
	pack string
	Check
}

type registry struct {
	checks []registered
	// engine and methods are what the handshake announced.
	engine  string
	methods map[string]bool
	conn    *conn
}

var global registry

var (
	idPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	sincePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Register adds a check, attributed to the pack whose checks package calls
// it. It panics on a malformed or duplicate check, which fails the build's
// first run rather than hiding the check.
func Register(c Check) {
	pack := ""
	if pc, _, _, ok := runtime.Caller(1); ok {
		if fn := runtime.FuncForPC(pc); fn != nil {
			pack = packOf(fn.Name())
		}
	}
	global.add(pack, c)
}

// generatedPacks is where the engine's generated module puts each pack's
// checks package.
const generatedPacks = "claudinite.checks/build/packs/"

// packOf reads the pack token from a function name in a pack's checks
// package: claudinite.checks/build/packs/<id>[/sub].<func>, or
// .../packs/local/<name>[/sub].<func> for a local pack, whose token is
// local/<name>.
func packOf(fn string) string {
	rest, ok := strings.CutPrefix(fn, generatedPacks)
	if !ok {
		return ""
	}
	if i := strings.Index(rest, "."); i >= 0 {
		rest = rest[:i]
	}
	parts := strings.Split(rest, "/")
	if parts[0] == "local" && len(parts) > 1 {
		return "local/" + parts[1]
	}
	return parts[0]
}

func (r *registry) add(pack string, c Check) {
	switch {
	case !idPattern.MatchString(c.ID):
		panic(fmt.Sprintf("checksdk: check id %q is not lowercase letters, digits and dashes", c.ID))
	case c.Run == nil && c.Judge == nil:
		panic(fmt.Sprintf("checksdk: check %s has no Run and no Judge", c.ID))
	case len(c.Tags) == 0:
		panic(fmt.Sprintf("checksdk: check %s has no tags", c.ID))
	case c.Judge != nil && !c.hookEvent():
		panic(fmt.Sprintf("checksdk: check %s has a Judge but no hook-event tag (%s)", c.ID, strings.Join(HookEvents, ", ")))
	case c.Judge == nil && c.hookEvent():
		panic(fmt.Sprintf("checksdk: check %s carries a hook-event tag but has no Judge", c.ID))
	case c.OnFail != "" && c.OnFail != "block" && c.OnFail != "advise":
		panic(fmt.Sprintf("checksdk: check %s: OnFail is block or advise, not %q", c.ID, c.OnFail))
	case c.Since != "" && !sincePattern.MatchString(c.Since):
		panic(fmt.Sprintf("checksdk: check %s: Since is the date it was added, YYYY-MM-DD, not %q", c.ID, c.Since))
	}
	for _, e := range r.checks {
		if e.pack == pack && e.ID == c.ID {
			panic(fmt.Sprintf("checksdk: check %s/%s is registered twice", pack, c.ID))
		}
	}
	r.checks = append(r.checks, registered{pack, c})
}

type request struct {
	Proto   string   `json:"proto"`
	Engine  string   `json:"engine"`
	Methods []string `json:"methods"`
	Op      string   `json:"op"`
	Tags    []string `json:"tags"`
	Pack    string   `json:"pack"`
	Repo    string   `json:"repo"`
	Event   string   `json:"event"`
	Call    Call     `json:"call"`
}

type listed struct {
	Check  string   `json:"check"`
	Tags   []string `json:"tags"`
	Judge  bool     `json:"judge,omitempty"`
	OnFail string   `json:"on_fail"`
	Since  string   `json:"since,omitempty"`
}

type reported struct {
	Check string `json:"check"`
	Finding
	OnFail string `json:"on_fail"`
	Since  string `json:"since,omitempty"`
}

type response struct {
	Proto    string     `json:"proto,omitempty"`
	Checks   []listed   `json:"checks,omitempty"`
	Findings []reported `json:"findings"`
	Errors   []string   `json:"errors,omitempty"`
	Error    string     `json:"error,omitempty"`
}

// Main serves the pipe on stdin and stdout and exits when stdin closes.
func Main() {
	if ms, err := strconv.Atoi(os.Getenv("CLAUDINITE_CHECK_DEADLINE_MS")); err == nil && ms > 0 {
		CheckDeadline = time.Duration(ms) * time.Millisecond
	}
	os.Exit(global.serve(os.Stdin, os.Stdout))
}

func (r *registry) serve(in io.Reader, out io.Writer) int {
	r.conn = newConn(in, out)
	shook := false
	for raw := range r.conn.requests {
		var req request
		if err := json.Unmarshal(raw, &req); err != nil {
			r.conn.send(response{Error: "malformed request: " + err.Error()})
			return 1
		}
		if !shook {
			if req.Proto != Proto {
				r.conn.send(response{Error: fmt.Sprintf("protocol %q is not %s", req.Proto, Proto)})
				return 1
			}
			shook = true
			r.engine = req.Engine
			r.methods = map[string]bool{}
			for _, m := range req.Methods {
				r.methods[m] = true
			}
			r.conn.send(response{Proto: Proto})
			continue
		}
		switch req.Op {
		case "list":
			resp := response{Checks: []listed{}}
			for _, c := range r.checks {
				resp.Checks = append(resp.Checks, listed{c.pack + "/" + c.ID, c.Tags, c.Judge != nil, c.onFail(), c.Since})
			}
			r.conn.send(resp)
		case "run":
			r.conn.send(r.run(req))
		case "judge":
			r.conn.send(r.judge(req))
		default:
			r.conn.send(response{Error: fmt.Sprintf("unknown op %q", req.Op)})
		}
	}
	if err := r.conn.err(); err != nil {
		r.conn.send(response{Error: err.Error()})
		return 1
	}
	return 0
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			found = found || h == w
		}
		if !found {
			return false
		}
	}
	return true
}

// repo is the Repo one request's checks share: its engine calls are
// memoised across them.
func (r *registry) repo(root string) Repo {
	return Repo{Root: root, st: newState(pipeHandler{r.conn}, r.methods, r.engine)}
}

func (r *registry) run(req request) response {
	resp := response{Findings: []reported{}}
	repo := r.repo(req.Repo)
	for _, c := range r.checks {
		if c.Run == nil || (req.Pack != "" && c.pack != req.Pack) || !hasAll(c.Tags, req.Tags) {
			continue
		}
		resp.collect(c, "run", repo, func(repo Repo) []Finding { return c.Run(repo) })
	}
	return resp
}

func (r *registry) judge(req request) response {
	resp := response{Findings: []reported{}}
	repo := r.repo(req.Repo)
	for _, c := range r.checks {
		if c.Judge == nil || !hasAll(c.Tags, []string{req.Event}) {
			continue
		}
		resp.collect(c, "judge", repo, func(repo Repo) []Finding { return c.Judge(repo, req.Call) })
	}
	return resp
}

// collect adds what f reports for c, or its panic or its running past
// CheckDeadline as an error. The deadline is the check's own work: its
// clock stops while it waits on an engine answer, up to MaxEngineWaits
// deadlines' worth of waiting, so a stream of calls cannot run it forever.
func (resp *response) collect(c registered, op string, repo Repo, f func(Repo) []Finding) {
	name := c.pack + "/" + c.ID
	type outcome struct {
		fs  []Finding
		err string
	}
	done := make(chan outcome, 1)
	clk := newClock()
	repo.clk = clk
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- outcome{err: fmt.Sprintf("%s: %v", name, panicText(p))}
			}
		}()
		done <- outcome{fs: f(repo)}
	}()
	timer := time.NewTimer(CheckDeadline)
	defer timer.Stop()
	var o outcome
wait:
	for {
		select {
		case o = <-done:
			break wait
		case <-timer.C:
			own, waited := clk.read()
			waitCap := MaxEngineWaits * CheckDeadline
			if own >= CheckDeadline || waited >= waitCap {
				o.err = fmt.Sprintf("%s: deadline (%v) passed in %s (%d ms waiting on the engine)", name, CheckDeadline, op, waited.Milliseconds())
				break wait
			}
			timer.Reset(max(min(CheckDeadline-own, waitCap-waited), 10*time.Millisecond))
		}
	}
	if o.err != "" {
		resp.Errors = append(resp.Errors, o.err)
		return
	}
	for _, fd := range o.fs {
		switch fd.Class {
		case ClassFinding, ClassAdvisory:
		case "":
			fd.Class = ClassFinding
			if c.onFail() == "advise" {
				fd.Class = ClassAdvisory
			}
		default:
			fd.Class = ClassFinding
		}
		if fd.Why == "" {
			fd.Why = c.Why
		}
		resp.Findings = append(resp.Findings, reported{name, fd, c.onFail(), c.Since})
	}
}

// missing is the panic a Repo method raises when the engine does not
// answer its call: the check's error says so, never "panic".
type missing string

func panicText(p any) string {
	if m, ok := p.(missing); ok {
		return string(m)
	}
	return fmt.Sprintf("panic: %v", p)
}
