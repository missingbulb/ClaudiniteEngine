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
// JSON object per line: first a handshake {"proto": Proto, "engine": ...}
// answered with the same proto, then any number of requests, one at a
// time:
//
//	{"op":"list"}                                   -> {"checks":[{"check":"<pack>/<id>","tags":[...],"judge":true}]}
//	{"op":"run","tags":[...],"pack":"","repo":DIR}  -> {"findings":[{"check","class","path","sentence"}],"errors":[...]}
//	{"op":"judge","event":E,"call":{...},"repo":DIR} -> the same answer as run
//
// A run selects every check with a Run whose tags include each requested
// tag, from one pack when pack is set. A judge op selects every check with
// a Judge tagged with its event (pre-tool-use, post-tool-use or
// user-prompt-submit) and hands it the call: the tool's name, its input
// and, after it ran, its response, as Claude Code sent them, or the
// prompt. A check that panics is reported in errors and the others still
// run. Lines are capped at 16 MiB.
//
// Its import path is claudinite.com/checksdk, which the engine resolves to
// the copy it unpacks into its cache, never the network.
package checksdk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Proto is the pipe's protocol version.
const Proto = "claudinite-checks-v1"

// MaxLine bounds one message on the pipe.
const MaxLine = 16 << 20

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
// repo root), in a sentence a person fixes it from.
type Finding struct {
	Class    Class  `json:"class"`
	Path     string `json:"path"`
	Sentence string `json:"sentence"`
}

// Repo is the repository a run checks.
type Repo struct {
	Root string
}

// Path is rel under the repo root.
func (r Repo) Path(rel string) string { return filepath.Join(r.Root, filepath.FromSlash(rel)) }

// Exists reports whether rel exists under the repo root.
func (r Repo) Exists(rel string) bool {
	_, err := os.Stat(r.Path(rel))
	return err == nil
}

// ReadFile reads rel under the repo root.
func (r Repo) ReadFile(rel string) ([]byte, error) { return os.ReadFile(r.Path(rel)) }

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
	Run  func(Repo) []Finding
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

type registered struct {
	pack string
	Check
}

type registry struct{ checks []registered }

var global registry

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

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

// packOf reads the pack id from a function name in a pack's checks
// package: claudinite.checks/build/packs/<id>[/sub].<func>.
func packOf(fn string) string {
	rest, ok := strings.CutPrefix(fn, generatedPacks)
	if !ok {
		return ""
	}
	if i := strings.IndexAny(rest, "./"); i >= 0 {
		rest = rest[:i]
	}
	return rest
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
	}
	for _, e := range r.checks {
		if e.pack == pack && e.ID == c.ID {
			panic(fmt.Sprintf("checksdk: check %s/%s is registered twice", pack, c.ID))
		}
	}
	r.checks = append(r.checks, registered{pack, c})
}

type request struct {
	Proto string   `json:"proto"`
	Op    string   `json:"op"`
	Tags  []string `json:"tags"`
	Pack  string   `json:"pack"`
	Repo  string   `json:"repo"`
	Event string   `json:"event"`
	Call  Call     `json:"call"`
}

type listed struct {
	Check string   `json:"check"`
	Tags  []string `json:"tags"`
	Judge bool     `json:"judge,omitempty"`
}

type reported struct {
	Check string `json:"check"`
	Finding
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
	os.Exit(global.serve(os.Stdin, os.Stdout))
}

func (r *registry) serve(in io.Reader, out io.Writer) int {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), MaxLine)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	shook := false
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = enc.Encode(response{Error: "malformed request: " + err.Error()})
			return 1
		}
		if !shook {
			if req.Proto != Proto {
				_ = enc.Encode(response{Error: fmt.Sprintf("protocol %q is not %s", req.Proto, Proto)})
				return 1
			}
			shook = true
			_ = enc.Encode(response{Proto: Proto})
			continue
		}
		switch req.Op {
		case "list":
			resp := response{Checks: []listed{}}
			for _, c := range r.checks {
				resp.Checks = append(resp.Checks, listed{c.pack + "/" + c.ID, c.Tags, c.Judge != nil})
			}
			_ = enc.Encode(resp)
		case "run":
			_ = enc.Encode(r.run(req))
		case "judge":
			_ = enc.Encode(r.judge(req))
		default:
			_ = enc.Encode(response{Error: fmt.Sprintf("unknown op %q", req.Op)})
		}
	}
	if err := sc.Err(); err != nil {
		_ = enc.Encode(response{Error: err.Error()})
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

func (r *registry) run(req request) response {
	resp := response{Findings: []reported{}}
	repo := Repo{Root: req.Repo}
	for _, c := range r.checks {
		if c.Run == nil || (req.Pack != "" && c.pack != req.Pack) || !hasAll(c.Tags, req.Tags) {
			continue
		}
		resp.collect(c, func() []Finding { return c.Run(repo) })
	}
	return resp
}

func (r *registry) judge(req request) response {
	resp := response{Findings: []reported{}}
	repo := Repo{Root: req.Repo}
	for _, c := range r.checks {
		if c.Judge == nil || !hasAll(c.Tags, []string{req.Event}) {
			continue
		}
		resp.collect(c, func() []Finding { return c.Judge(repo, req.Call) })
	}
	return resp
}

// collect adds what f reports for c, or its panic as an error.
func (resp *response) collect(c registered, f func() []Finding) {
	name := c.pack + "/" + c.ID
	defer func() {
		if p := recover(); p != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: panic: %v", name, p))
		}
	}()
	for _, fd := range f() {
		if fd.Class != ClassFinding && fd.Class != ClassAdvisory {
			fd.Class = ClassFinding
		}
		resp.Findings = append(resp.Findings, reported{name, fd})
	}
}
