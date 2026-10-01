// Package run starts the checks binary as a child process and talks to it
// over its stdin and stdout, one JSON object per line: a handshake naming
// the protocol, then one request. The child gets a scrubbed environment
// with NODE_OPTIONS unset, so no secret reaches pack code; a malformed or
// oversized line, or a silence longer than the limit, kills it.
package run

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
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

	extraEnv []string
}

// Finding is one finding a check reported.
type Finding struct {
	Check    string `json:"check"`
	Class    string `json:"class"`
	Path     string `json:"path"`
	Sentence string `json:"sentence"`
}

// Listed is one check the binary holds.
type Listed struct {
	Check string   `json:"check"`
	Tags  []string `json:"tags"`
}

// Result is a run's answer. Err is set when the run itself failed; Errors
// are checks that failed inside a run that completed.
type Result struct {
	Findings []Finding
	Errors   []string
	Err      error
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
		out = append(out, findings.Finding{Class: class, ID: f.Check, Path: f.Path, Sentence: f.Sentence})
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
// and the home directory, never a token, and no NODE_OPTIONS.
func (r Runner) childEnv() []string {
	var env []string
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SYSTEMROOT", "USERPROFILE"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return append(env, r.extraEnv...)
}

func (r Runner) converse(req any) (answer, error) {
	silence := r.Silence
	if silence == 0 {
		silence = DefaultSilence
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Binary)
	cmd.Env = r.childEnv()
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return answer{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return answer{}, err
	}
	if err := cmd.Start(); err != nil {
		return answer{}, err
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
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
	ask := func(v any) (answer, error) {
		if err := enc.Encode(v); err != nil {
			return answer{}, fmt.Errorf("writing to the checks binary: %w", err)
		}
		select {
		case line := <-lines:
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
	hs, err := ask(map[string]string{"proto": Proto, "engine": r.Engine})
	if err != nil {
		return answer{}, err
	}
	if hs.Proto != Proto {
		return answer{}, fmt.Errorf("the checks binary speaks protocol %q, not %s", hs.Proto, Proto)
	}
	a, err := ask(req)
	_ = stdin.Close()
	return a, err
}

// Run runs every check whose tags include each of tags, from pack when it
// is set, over repo. The breadcrumb is `[cn] checks <event> <outcome>`:
// ok, error (the run failed or a check failed inside it) or timeout.
func (r Runner) Run(event string, tags []string, pack, repo string) (Result, string) {
	start := time.Now()
	if tags == nil {
		tags = []string{}
	}
	a, err := r.converse(map[string]any{"op": "run", "tags": tags, "pack": pack, "repo": repo})
	res := Result{Findings: a.Findings, Errors: a.Errors, Err: err}
	outcome := breadcrumb.OK
	switch {
	case errors.Is(err, ErrSilent):
		outcome = breadcrumb.Timeout
	case err != nil || len(a.Errors) > 0:
		outcome = breadcrumb.Error
	}
	return res, breadcrumb.Line("checks", event, outcome, time.Since(start))
}

// List returns every check the binary holds.
func (r Runner) List() ([]Listed, error) {
	a, err := r.converse(map[string]string{"op": "list"})
	return a.Checks, err
}
