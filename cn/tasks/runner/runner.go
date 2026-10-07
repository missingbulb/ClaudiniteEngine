// Package runner is the task step's Node half and the engine side of its
// pipe. The runner script, its resolve hook and @claudinite/sdk are
// embedded here and unpacked read-only into the engine's cache on first
// use; a worker module (a task's code_worker_mjs) and a task-local
// preconditions.mjs run in `node --import register.mjs runner.mjs` with
// the task directory as cwd. The engine and the child exchange one JSON
// object per line over the child's stdin and stdout, the framing of the
// coded checks' pipe: a handshake naming the protocol and the methods the
// engine answers, one request, and the SDK's calls answered while the
// request is worked. A code_work command runs through the shell with no
// pipe. Either way the child's output is echoed as it arrives, its
// timeout is a hard kill of its whole process group, and its environment
// is exactly the one the caller built, NODE_OPTIONS never among it.
package runner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Proto is the pipe's protocol, which runner.mjs answers.
const Proto = "claudinite-tasks-v1"

// MaxLine bounds one line on the pipe.
const MaxLine = 16 << 20

//go:embed js
var embedded embed.FS

// Files maps each embedded file's path under js/ to its content.
func Files() map[string][]byte {
	out := map[string][]byte{}
	_ = fs.WalkDir(embedded, "js", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := embedded.ReadFile(p)
		out[strings.TrimPrefix(p, "js/")] = b
		return nil
	})
	return out
}

// Hash is the embedded copy's identity: sha256 over each path and content
// in path order.
func Hash() string {
	files := Files()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(files[n]))
		h.Write(files[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Unpack writes the embedded copy under root, in a folder named for its
// hash, read-only, and returns that folder. A folder already there whose
// files differ from the embedded copy is replaced, never trusted.
func Unpack(root string) (string, error) {
	hash := Hash()
	dir := filepath.Join(root, "runner-"+hash[:16])
	if intact(dir) {
		return dir, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".runner-")
	if err != nil {
		return "", err
	}
	defer func() { _ = makeWritable(tmp); _ = os.RemoveAll(tmp) }()
	for name, b := range Files() {
		p := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, b, 0o444); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(dir); err == nil {
		_ = makeWritable(dir)
		if err := os.RemoveAll(dir); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if intact(dir) {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

func intact(dir string) bool {
	for name, b := range Files() {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, b) {
			return false
		}
	}
	return true
}

func makeWritable(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			_ = os.Chmod(p, 0o644)
		}
		return nil
	})
}

// Server answers the SDK calls a child makes while its request is worked.
type Server interface {
	// Methods are the calls it answers, which the handshake announces.
	Methods() []string
	// Handle answers one call with a JSON value, or an error the script reads.
	Handle(method string, args json.RawMessage) (any, error)
}

// Runner runs task steps with one Node binary and one unpacked copy.
type Runner struct {
	// Node is the node binary; "node" on PATH when empty.
	Node string
	// Dir is the unpacked copy (Unpack).
	Dir string
	// Engine is the engine version the handshake names.
	Engine string
}

// Step is one child: where it runs, with what environment, for how long,
// and where its output goes as it arrives.
type Step struct {
	// Dir is the child's working directory, the task folder.
	Dir string
	// Env is the child's whole environment; NODE_OPTIONS is dropped.
	Env     []string
	Timeout time.Duration
	// Echo receives each line of output as it arrives, stream "stdout" or
	// "stderr"; nil echoes nothing.
	Echo   func(stream, line string)
	Server Server
}

// Result is a finished step. Output is everything the child printed,
// both streams, which the queue's markers are read from; Err is a step
// that could not run or broke the protocol.
type Result struct {
	OK       bool
	TimedOut bool
	// Code is the exit code, -1 when the child was killed or never ran.
	Code   int
	Output string
	Err    error
	// Calls counts the SDK calls by method.
	Calls map[string]int
}

// Why is the one-line reason a failed step gives.
func (r Result) Why(kind string) string {
	switch {
	case r.TimedOut:
		return kind + " exceeded its timeout and was killed"
	case r.Err != nil:
		return kind + " could not run: " + r.Err.Error()
	case r.Code >= 0:
		return fmt.Sprintf("%s exited %d", kind, r.Code)
	}
	return kind + " could not run"
}

// Work runs a worker module through the runner.
func (r Runner) Work(s Step, module string, secrets []string, automerge string) Result {
	if secrets == nil {
		secrets = []string{}
	}
	req := map[string]any{"op": "work", "module": module, "secrets": secrets, "automerge": automerge}
	var answer struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	res := r.converse(s, req, &answer)
	switch {
	case res.Err != nil || res.TimedOut:
		res.OK = false
	case answer.Error != "":
		res.OK, res.Err = false, errors.New(answer.Error)
	case answer.OK == nil:
		res.OK, res.Err = false, errors.New("the runner answered no verdict")
	default:
		res.OK = *answer.OK && res.Code == 0
	}
	return res
}

// Ref is one reference to a task-local term, as the expression wrote it.
type Ref struct {
	Name string  `json:"name"`
	Arg  *string `json:"arg"`
	Text string  `json:"text"`
}

// Outcome is one term's answer.
type Outcome struct {
	Holds   bool     `json:"holds"`
	Reason  string   `json:"reason,omitempty"`
	Context []string `json:"context,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// TermsInput is what every term reads, collected once.
type TermsInput struct {
	Signals    any            `json:"signals"`
	Config     map[string]any `json:"config"`
	Item       any            `json:"item"`
	WindowDays float64        `json:"windowDays"`
	Now        string         `json:"now"`
}

// Terms answers every reference in one call to the task's
// preconditions.mjs, keyed by the reference's text.
func (r Runner) Terms(s Step, file string, refs []Ref, in TermsInput) (map[string]Outcome, error) {
	if in.Config == nil {
		in.Config = map[string]any{}
	}
	req := map[string]any{"op": "terms", "file": file, "refs": refs, "signals": in.Signals,
		"config": in.Config, "item": in.Item, "windowDays": in.WindowDays, "now": in.Now}
	var answer struct {
		Outcomes map[string]Outcome `json:"outcomes"`
		Error    string             `json:"error"`
	}
	res := r.converse(s, req, &answer)
	switch {
	case res.TimedOut:
		return nil, errors.New("the precondition terms exceeded their timeout and were killed")
	case res.Err != nil:
		return nil, res.Err
	case answer.Error != "":
		return nil, errors.New(answer.Error)
	}
	return answer.Outcomes, nil
}

// Shell runs a code_work command through the platform's shell, with no
// pipe: its output is the whole channel back.
func Shell(s Step, command string) Result {
	name, args := "/bin/sh", []string{"-c", command}
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/d", "/s", "/c", command}
	}
	res := Result{Code: -1, Calls: map[string]int{}}
	if _, err := os.Stat(s.Dir); err != nil {
		res.Err = fmt.Errorf("task directory %s does not exist — nothing was run", s.Dir)
		return res
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = s.Dir, scrubbed(s.Env)
	cmd.SysProcAttr = groupAttr()
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second
	var out output
	cmd.Stdout = out.writer("stdout", s.Echo)
	cmd.Stderr = out.writer("stderr", s.Echo)
	if err := cmd.Start(); err != nil {
		res.Err = err
		return res
	}
	var killed atomic.Bool
	timer := time.AfterFunc(s.Timeout, func() { killed.Store(true); cancel() })
	err := cmd.Wait()
	timer.Stop()
	out.flush()
	res.TimedOut = killed.Load()
	res.Output = out.String()
	res.Code = exitCode(err, cmd)
	res.OK = err == nil && !res.TimedOut
	return res
}

// converse runs the runner with one request and decodes its answer.
func (r Runner) converse(s Step, req any, answer any) Result {
	res := Result{Code: -1, Calls: map[string]int{}}
	if _, err := os.Stat(s.Dir); err != nil {
		res.Err = fmt.Errorf("task directory %s does not exist — nothing was run", s.Dir)
		return res
	}
	node := r.Node
	if node == "" {
		node = "node"
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--import", fileURL(filepath.Join(r.Dir, "register.mjs")), filepath.Join(r.Dir, "runner.mjs"))
	cmd.Dir, cmd.Env = s.Dir, scrubbed(s.Env)
	cmd.SysProcAttr = groupAttr()
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second
	var out output
	cmd.Stderr = out.writer("stderr", s.Echo)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		res.Err = err
		return res
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		res.Err = err
		return res
	}
	if err := cmd.Start(); err != nil {
		res.Err = err
		return res
	}
	var killed atomic.Bool
	timer := time.AfterFunc(s.Timeout, func() { killed.Store(true); cancel() })
	defer timer.Stop()

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
			err = fmt.Errorf("a line from the runner is too long (over %d bytes)", MaxLine)
		} else if err == nil {
			err = errors.New("the runner exited before answering")
		}
		scanErr <- err
	}()
	enc := json.NewEncoder(stdin)
	enc.SetEscapeHTML(false)
	var mu sync.Mutex
	send := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(v)
	}
	ask := func(v any, into any) error {
		if err := send(v); err != nil {
			return fmt.Errorf("writing to the runner: %w", err)
		}
		for {
			select {
			case line := <-lines:
				var probe struct {
					SDK   *string         `json:"sdk"`
					ID    json.RawMessage `json:"id"`
					Args  json.RawMessage `json:"args"`
					Proto *string         `json:"proto"`
					OK    *bool           `json:"ok"`
				}
				if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &probe) != nil {
					out.line("stdout", string(line), s.Echo)
					continue
				}
				if probe.SDK != nil {
					if err := send(serve(s.Server, *probe.SDK, probe.ID, probe.Args, res.Calls)); err != nil {
						return fmt.Errorf("writing to the runner: %w", err)
					}
					continue
				}
				if err := json.Unmarshal(line, into); err != nil {
					return fmt.Errorf("malformed line from the runner: %v", err)
				}
				return nil
			case err := <-scanErr:
				return err
			case <-ctx.Done():
				return errors.New("killed")
			}
		}
	}
	methods := []string{}
	if s.Server != nil {
		methods = s.Server.Methods()
	}
	var hello struct {
		Proto string `json:"proto"`
		Error string `json:"error"`
	}
	err = ask(map[string]any{"proto": Proto, "engine": r.Engine, "methods": methods}, &hello)
	switch {
	case err != nil:
	case hello.Error != "":
		err = errors.New(hello.Error)
	case hello.Proto != Proto:
		err = fmt.Errorf("the runner speaks protocol %q, not %s", hello.Proto, Proto)
	default:
		err = ask(req, answer)
	}
	_ = stdin.Close()
	if err != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	out.flush()
	res.TimedOut = killed.Load()
	res.Output = out.String()
	res.Code = exitCode(waitErr, cmd)
	if !res.TimedOut {
		res.Err = err
	}
	return res
}

type sdkAnswer struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result"`
	Error  *string         `json:"error,omitempty"`
}

func serve(srv Server, method string, id, args json.RawMessage, calls map[string]int) sdkAnswer {
	calls[method]++
	known := false
	if srv != nil {
		for _, m := range srv.Methods() {
			known = known || m == method
		}
	}
	if !known {
		e := "unknown method " + method
		return sdkAnswer{ID: id, Error: &e}
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	v, err := srv.Handle(method, args)
	if err != nil {
		e := err.Error()
		return sdkAnswer{ID: id, Error: &e}
	}
	return sdkAnswer{ID: id, Result: v}
}

// scrubbed is env without NODE_OPTIONS.
func scrubbed(env []string) []string {
	out := []string{}
	for _, kv := range env {
		if !strings.HasPrefix(kv, "NODE_OPTIONS=") {
			out = append(out, kv)
		}
	}
	return out
}

func exitCode(err error, cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	if code := cmd.ProcessState.ExitCode(); code >= 0 {
		return code
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

// output collects both streams and echoes each line as it completes.
type output struct {
	mu  sync.Mutex
	buf strings.Builder
	// partial holds each stream's unfinished line.
	partial map[string]*strings.Builder
	echo    map[string]func(string, string)
}

func (o *output) line(stream, l string, echo func(string, string)) {
	o.mu.Lock()
	o.buf.WriteString(l + "\n")
	o.mu.Unlock()
	if echo != nil {
		echo(stream, l)
	}
}

type streamWriter struct {
	o      *output
	stream string
	echo   func(string, string)
}

func (w streamWriter) Write(p []byte) (int, error) {
	w.o.mu.Lock()
	if w.o.partial == nil {
		w.o.partial = map[string]*strings.Builder{}
		w.o.echo = map[string]func(string, string){}
	}
	b := w.o.partial[w.stream]
	if b == nil {
		b = &strings.Builder{}
		w.o.partial[w.stream] = b
		w.o.echo[w.stream] = w.echo
	}
	b.Write(p)
	text := b.String()
	var done []string
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			break
		}
		done = append(done, strings.TrimSuffix(text[:i], "\r"))
		text = text[i+1:]
	}
	b.Reset()
	b.WriteString(text)
	w.o.mu.Unlock()
	for _, l := range done {
		w.o.line(w.stream, l, w.echo)
	}
	return len(p), nil
}

func (o *output) writer(stream string, echo func(string, string)) io.Writer {
	return streamWriter{o: o, stream: stream, echo: echo}
}

func (o *output) flush() {
	o.mu.Lock()
	var rest [][2]string
	for stream, b := range o.partial {
		if b.Len() > 0 {
			rest = append(rest, [2]string{stream, b.String()})
			b.Reset()
		}
	}
	echo := o.echo
	o.mu.Unlock()
	for _, r := range rest {
		o.line(r[0], r[1], echo[r[0]])
	}
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}
