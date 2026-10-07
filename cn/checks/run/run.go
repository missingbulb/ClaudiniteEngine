// Package run starts the checks binary as a child process and talks to it
// over its stdin and stdout, one JSON object per line: a handshake naming
// the protocol and the SDK methods the engine answers, then one request.
// While the answer is awaited the child may call back: a line carrying
// "sdk" is a call, which the Runner's Server answers on the child's stdin
// before it goes on waiting, one call in flight at a time. The child gets
// a scrubbed environment with NODE_OPTIONS unset, so no secret reaches
// pack code; a malformed or oversized line, or a silence longer than the
// limit, kills it.
package run

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
)

// Proto is the pipe's protocol, which the SDK's Main answers.
const Proto = "claudinite-checks-v1"

// MaxLine bounds one line from the child.
const MaxLine = 16 << 20

// DefaultSilence is how long the child may go without answering.
const DefaultSilence = 60 * time.Second

// Runner runs one checks binary.
type Runner struct {
	Binary string
	Engine string
	// Silence is the longest the child may take to answer a line;
	// DefaultSilence when zero.
	Silence time.Duration
	// Server answers the child's SDK calls; nil answers every call with
	// an error and announces no methods.
	Server Server

	extraEnv []string
}

// Server answers the SDK calls a child makes while a request is answered.
type Server interface {
	// Methods are the calls it answers, which the handshake announces.
	Methods() []string
	// Handle answers one call with JSON, or an error the check reads.
	Handle(method string, args json.RawMessage) (json.RawMessage, error)
}

// Finding is one finding a check reported, with the check's on_fail and
// since.
type Finding struct {
	Check    string `json:"check"`
	Class    string `json:"class"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Sentence string `json:"sentence"`
	Why      string `json:"why"`
	Fix      string `json:"fix"`
	OnFail   string `json:"on_fail"`
	Since    string `json:"since"`
}

// Listed is one check the binary holds; Judge marks a hook judge.
type Listed struct {
	Check  string   `json:"check"`
	Tags   []string `json:"tags"`
	Judge  bool     `json:"judge"`
	OnFail string   `json:"on_fail"`
	Since  string   `json:"since"`
}

// Call is the call a judge reads, as the SDK's Call: the tool, its input
// and response as Claude Code sent them, or the prompt.
type Call struct {
	Tool     string          `json:"tool,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Prompt   string          `json:"prompt,omitempty"`
}

// Result is a run's answer. Err is set when the run itself failed; Errors
// are checks that failed inside a run that completed. Calls counts the
// child's SDK calls by method, and SDKCrumb is the run's
// `[cn] sdk <event> <outcome>` breadcrumb when a Server was attached.
type Result struct {
	Findings []Finding
	Errors   []string
	Err      error
	Calls    map[string]int
	SDKCrumb string
	// Stderr is the tail of what the child wrote to stderr, at most
	// StderrTail bytes.
	Stderr string
}

// StderrTail bounds how much of the child's stderr is kept.
const StderrTail = 4 << 10

// tail keeps the last StderrTail bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - StderrTail; over > 0 {
		t.buf = append([]byte{}, t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// summary is the tail in one line: its last non-empty line, after the
// panic line when there is one.
func summary(text string) string {
	var last, panicLine string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		last = l
		if panicLine == "" && strings.HasPrefix(l, "panic: ") {
			panicLine = l
		}
	}
	if panicLine != "" && panicLine != last {
		return panicLine + " … " + last
	}
	return last
}

// Blocking reports whether any finding has class finding.
func (r Result) Blocking() bool {
	for _, f := range r.Findings {
		if f.Class != "advisory" {
			return true
		}
	}
	return false
}

// Shared converts the findings to the engine's finding shape: a check's
// finding blocks like a break, an advisory is reported like a
// deprecation.
func (r Result) Shared() []findings.Finding {
	var out []findings.Finding
	for _, f := range r.Findings {
		class := findings.Coded
		if f.Class == "advisory" {
			class = findings.Advisory
		}
		pack, id := "", f.Check
		if i := strings.LastIndex(f.Check, "/"); i >= 0 {
			pack, id = f.Check[:i], f.Check[i+1:]
		}
		out = append(out, findings.Finding{Class: class, ID: id, Pack: pack, Path: f.Path, Line: f.Line, Sentence: f.Sentence, Why: f.Why, Fix: f.Fix})
	}
	return out
}

type answer struct {
	Proto    string    `json:"proto"`
	Checks   []Listed  `json:"checks"`
	Findings []Finding `json:"findings"`
	Errors   []string  `json:"errors"`
	Error    string    `json:"error"`
}

// ErrSilent is the child going quiet past the limit.
var ErrSilent = errors.New("the checks binary fell silent")

// childEnv is the child's whole environment: enough to find a temp folder
// and the home directory, and the per-check clock's override, never a
// token, and no NODE_OPTIONS.
func (r Runner) childEnv() []string {
	var env []string
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SYSTEMROOT", "USERPROFILE", "CLAUDINITE_CHECK_DEADLINE_MS"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return append(env, r.extraEnv...)
}

// talk is one child's conversation: the SDK calls it made and whether
// any named a method the Server does not answer.
type talk struct {
	calls   map[string]int
	unknown bool
}

// sdkLine is a line from the child that may be an SDK call.
type sdkLine struct {
	SDK  *string         `json:"sdk"`
	ID   json.RawMessage `json:"id"`
	Args json.RawMessage `json:"args"`
}

type sdkAnswer struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *string         `json:"error,omitempty"`
}

// serve answers one SDK call.
func (r Runner) serve(c sdkLine, tk *talk) sdkAnswer {
	method := *c.SDK
	tk.calls[method]++
	known := false
	if r.Server != nil {
		for _, m := range r.Server.Methods() {
			known = known || m == method
		}
	}
	if !known {
		tk.unknown = true
		e := "unknown method " + method
		return sdkAnswer{ID: c.ID, Error: &e}
	}
	args := c.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	res, err := r.Server.Handle(method, args)
	if err != nil {
		e := err.Error()
		return sdkAnswer{ID: c.ID, Error: &e}
	}
	if len(res) == 0 {
		res = json.RawMessage("null")
	}
	return sdkAnswer{ID: c.ID, Result: res}
}

func (r Runner) methods() []string {
	if r.Server == nil {
		return []string{}
	}
	return r.Server.Methods()
}

func (r Runner) converse(req any) (a answer, tk talk, stderr string, err error) {
	tk = talk{calls: map[string]int{}}
	silence := r.Silence
	if silence == 0 {
		silence = DefaultSilence
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Binary)
	cmd.Env = r.childEnv()
	errTail := &tail{}
	cmd.Stderr = errTail
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return answer{}, tk, "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return answer{}, tk, "", err
	}
	if err := cmd.Start(); err != nil {
		return answer{}, tk, "", err
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
		stderr = errTail.String()
		if line := summary(stderr); err != nil && line != "" {
			err = fmt.Errorf("%w: %s", err, line)
		}
	}()
	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), MaxLine)
		for sc.Scan() {
			select {
			case lines <- append([]byte{}, sc.Bytes()...):
			case <-ctx.Done():
				return
			}
		}
		err := sc.Err()
		if errors.Is(err, bufio.ErrTooLong) {
			err = fmt.Errorf("a line from the checks binary is too long (over %d bytes)", MaxLine)
		} else if err == nil {
			err = errors.New("the checks binary exited before answering")
		}
		scanErr <- err
	}()
	enc := json.NewEncoder(stdin)
	enc.SetEscapeHTML(false)
	ask := func(v any) (answer, error) {
		if err := enc.Encode(v); err != nil {
			select {
			case e := <-scanErr:
				return answer{}, e
			case <-time.After(time.Second):
			}
			return answer{}, fmt.Errorf("writing to the checks binary: %w", err)
		}
		for {
			select {
			case line := <-lines:
				var call sdkLine
				if json.Unmarshal(line, &call) == nil && call.SDK != nil {
					if err := enc.Encode(r.serve(call, &tk)); err != nil {
						return answer{}, fmt.Errorf("writing to the checks binary: %w", err)
					}
					continue
				}
				var a answer
				if err := json.Unmarshal(line, &a); err != nil {
					return answer{}, fmt.Errorf("malformed line from the checks binary: %v", err)
				}
				if a.Error != "" {
					return answer{}, fmt.Errorf("the checks binary refused: %s", a.Error)
				}
				return a, nil
			case err := <-scanErr:
				return answer{}, err
			case <-time.After(silence):
				return answer{}, fmt.Errorf("%w for %v", ErrSilent, silence)
			}
		}
	}
	hs, err := ask(map[string]any{"proto": Proto, "engine": r.Engine, "methods": r.methods()})
	if err != nil {
		return answer{}, tk, "", err
	}
	if hs.Proto != Proto {
		return answer{}, tk, "", fmt.Errorf("the checks binary speaks protocol %q, not %s", hs.Proto, Proto)
	}
	a, err = ask(req)
	_ = stdin.Close()
	return a, tk, "", err
}

// Run runs every check whose tags include each of tags, from pack when it
// is set, over repo. The breadcrumb is `[cn] checks <event> <outcome>`:
// ok, error (the run failed or a check failed inside it) or timeout.
func (r Runner) Run(event string, tags []string, pack, repo string) (Result, string) {
	start := time.Now()
	if tags == nil {
		tags = []string{}
	}
	a, tk, stderr, err := r.converse(map[string]any{"op": "run", "tags": tags, "pack": pack, "repo": repo})
	res := Result{Findings: a.Findings, Errors: a.Errors, Err: err, Calls: tk.calls, Stderr: stderr}
	outcome := breadcrumb.OK
	switch {
	case errors.Is(err, ErrSilent):
		outcome = breadcrumb.Timeout
	case err != nil || len(a.Errors) > 0:
		outcome = breadcrumb.Error
	}
	if r.Server != nil {
		sdk := breadcrumb.OK
		switch {
		case errors.Is(err, ErrSilent):
			sdk = breadcrumb.Timeout
		case tk.unknown:
			sdk = breadcrumb.Error
		}
		res.SDKCrumb = breadcrumb.Line("sdk", event, sdk, time.Since(start))
	}
	return res, breadcrumb.Line("checks", event, outcome, time.Since(start))
}

// Judge asks the judges tagged with event about call. A child that
// fails or falls silent past the Runner's Silence is Err, which the caller
// treats as could not judge.
func (r Runner) Judge(event string, call Call, repo string) Result {
	a, tk, stderr, err := r.converse(map[string]any{"op": "judge", "event": event, "call": call, "repo": repo})
	return Result{Findings: a.Findings, Errors: a.Errors, Err: err, Calls: tk.calls, Stderr: stderr}
}

// List returns every check the binary holds.
func (r Runner) List() ([]Listed, error) {
	a, _, _, err := r.converse(map[string]string{"op": "list"})
	return a.Checks, err
}
