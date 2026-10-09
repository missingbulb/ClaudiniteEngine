// Package gitcmd runs the git binary for the updater: branch, commit,
// push, read a file at a ref, and list what a branch changed. Commits are
// made as github-actions[bot], the identity whose pin changes CI accepts.
// No token variable reaches a git child: a child that talks to the remote
// (fetch, push, ls-remote) gets the job token as an http.extraheader
// through GIT_CONFIG_* in its own environment, and nothing writes it to
// .git/config, so a program run in the checkout finds no credential there.
package gitcmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CommandTimeout bounds a local git command; RemoteTimeout one that talks
// to a remote (a push, a clone), which moves data a local read does not.
// A command past its bound is killed and fails naming its arguments.
var (
	CommandTimeout = 30 * time.Second
	RemoteTimeout  = 5 * time.Minute
)

// Faults collects the commands that timed out, for a caller whose reads
// report only ok; nil collects nothing. The first timeout also spends the
// run's git: every later command sharing these Faults fails at once
// without running, so one run waits on at most one CommandTimeout.
type Faults struct {
	mu    sync.Mutex
	list  []string
	first string
}

func (f *Faults) add(msg string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.list = append(f.list, msg)
	if f.first == "" {
		f.first = msg
	}
	f.mu.Unlock()
}

// spent is the error a command named by args fails with once a timeout
// has spent the run's git, else nil.
func (f *Faults) spent(args []string) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.first == "" {
		return nil
	}
	return &TimeoutError{fmt.Sprintf("git %s not run: %s", strings.Join(args, " "), f.first)}
}

// First is the first timeout, "" while none has spent the run's git.
func (f *Faults) First() string {
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.first
}

// Take returns the faults collected since the last Take.
func (f *Faults) Take() []string {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.list
	f.list = nil
	return out
}

// command is git with args under timeout; done reports a timeout as the
// error naming shown, else passes err through.
func command(timeout time.Duration, shown []string, args ...string) (cmd *exec.Cmd, done func(error) error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd = exec.CommandContext(ctx, "git", args...)
	cmd.WaitDelay = time.Second
	return cmd, func(err error) error {
		defer cancel()
		if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &TimeoutError{fmt.Sprintf("git %s timed out after %v", strings.Join(shown, " "), timeout)}
		}
		return err
	}
}

// TimeoutError is a git command killed at its bound.
type TimeoutError struct{ msg string }

func (e *TimeoutError) Error() string { return e.msg }

// Bot is the identity update commits carry.
const (
	BotName  = "github-actions[bot]"
	BotEmail = "41898282+github-actions[bot]@users.noreply.github.com"
)

// Repo is a working tree. Token, when set, authenticates the children
// that talk to the remote.
type Repo struct {
	Dir   string
	Token string
	// Faults, when set, collects every command that timed out.
	Faults *Faults
}

// extraheader is the config key that carries the token, as
// actions/checkout writes it when it persists credentials.
const extraheader = "http.https://github.com/.extraheader"

// secretEnv names variables no git child may inherit.
var secretEnv = []string{"GITHUB_TOKEN", "GH_TOKEN", "ACTIONS_RUNTIME_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "NODE_AUTH_TOKEN"}

func childEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		keep := !strings.HasPrefix(kv, "GIT_CONFIG_COUNT=") && !strings.HasPrefix(kv, "GIT_CONFIG_KEY_") && !strings.HasPrefix(kv, "GIT_CONFIG_VALUE_")
		for _, s := range secretEnv {
			if strings.HasPrefix(kv, s+"=") {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return append(out, "GIT_TERMINAL_PROMPT=0")
}

func (r Repo) run(args ...string) ([]byte, error) { return r.exec(false, args...) }

// remote runs a child that talks to the remote, with the token when set.
func (r Repo) remote(args ...string) ([]byte, error) { return r.exec(true, args...) }

func (r Repo) remoteLine(args ...string) (string, error) {
	out, err := r.remote(args...)
	return strings.TrimSpace(string(out)), err
}

func (r Repo) exec(remote bool, args ...string) ([]byte, error) {
	if err := r.Faults.spent(args); err != nil {
		return nil, err
	}
	cmd, done := r.child(remote, args)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err = done(err); err != nil {
		var te *TimeoutError
		if errors.As(err, &te) {
			r.Faults.add(te.msg)
			return out, te
		}
		return out, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// child is git with args in the tree, as the bot, under the bound its
// kind takes, with the token when it talks to the remote.
func (r Repo) child(remote bool, args []string) (*exec.Cmd, func(error) error) {
	timeout := CommandTimeout
	if remote {
		timeout = RemoteTimeout
	}
	cmd, done := command(timeout, args, append([]string{"-c", "user.name=" + BotName, "-c", "user.email=" + BotEmail}, args...)...)
	cmd.Dir = r.Dir
	cmd.Env = childEnv()
	if remote && r.Token != "" {
		cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + r.Token))
		// The empty first value clears any header .git/config persisted,
		// since the values accumulate.
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0="+extraheader, "GIT_CONFIG_VALUE_0=",
			"GIT_CONFIG_KEY_1="+extraheader, "GIT_CONFIG_VALUE_1=AUTHORIZATION: basic "+cred)
	}
	return cmd, done
}

func (r Repo) line(args ...string) (string, error) {
	out, err := r.run(args...)
	return strings.TrimSpace(string(out)), err
}

// Head is the checked-out commit.
func (r Repo) Head() (string, error) { return r.line("rev-parse", "HEAD") }

// CurrentBranch is the checked-out branch's name.
func (r Repo) CurrentBranch() (string, error) { return r.line("symbolic-ref", "--short", "HEAD") }

// RevParse resolves ref to a commit.
func (r Repo) RevParse(ref string) (string, error) {
	return r.line("rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

// CreateBranch checks out a new branch name at from, replacing a local
// branch of that name.
func (r Repo) CreateBranch(name, from string) error {
	_, err := r.run("checkout", "-q", "-B", name, from)
	return err
}

// Checkout checks out ref.
func (r Repo) Checkout(ref string) error {
	_, err := r.run("checkout", "-q", ref)
	return err
}

// Commit stages exactly paths and commits them.
func (r Repo) Commit(message string, paths ...string) error {
	if _, err := r.run(append([]string{"add", "--"}, paths...)...); err != nil {
		return err
	}
	_, err := r.run("commit", "-q", "-m", message, "--")
	return err
}

// Push pushes branch to remote, replacing a remote branch of that name,
// which only the updater writes.
func (r Repo) Push(remote, branch string) error {
	_, err := r.remote("push", "-q", "--force", remote, "refs/heads/"+branch+":refs/heads/"+branch)
	return err
}

// DeleteRemoteBranch deletes branch on remote; an already absent branch is
// not an error.
func (r Repo) DeleteRemoteBranch(remote, branch string) error {
	out, err := r.remote("ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil
	}
	_, err = r.remote("push", "-q", remote, "--delete", "refs/heads/"+branch)
	return err
}

// Fetch fetches refspecs from remote.
func (r Repo) Fetch(remote string, refspecs ...string) error {
	_, err := r.remote(append([]string{"fetch", "-q", remote}, refspecs...)...)
	return err
}

// Show returns path's content at ref, and false when ref has no such path.
func (r Repo) Show(ref, path string) ([]byte, bool, error) {
	if _, err := r.run("cat-file", "-e", ref+":"+path); err != nil {
		if _, rerr := r.RevParse(ref); rerr != nil {
			return nil, false, fmt.Errorf("%s is not a commit", ref)
		}
		return nil, false, nil
	}
	out, err := r.run("show", ref+":"+path)
	return out, err == nil, err
}

// Regular reports whether path at ref is a regular file, and false when ref
// has no such path. Show reads a symlink as its target's text.
func (r Repo) Regular(ref, path string) (bool, error) {
	out, err := r.run("ls-tree", "-z", ref, "--", path)
	if err != nil {
		return false, err
	}
	meta, _, ok := strings.Cut(strings.TrimSuffix(string(out), "\x00"), "\t")
	f := strings.Fields(meta)
	return ok && len(f) == 3 && f[1] == "blob" && (f[0] == "100644" || f[0] == "100755"), nil
}

// MergeBase is the best common ancestor of a and b.
func (r Repo) MergeBase(a, b string) (string, error) { return r.line("merge-base", a, b) }

// ChangedFiles lists the paths head changed since its merge base with base.
func (r Repo) ChangedFiles(base, head string) ([]string, error) {
	out, err := r.run("diff", "--name-only", "--no-renames", base+"..."+head)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			names = append(names, l)
		}
	}
	return names, nil
}

// DiffStat is git diff --stat of head since its merge base with base.
func (r Repo) DiffStat(base, head string) (string, error) {
	return r.line("diff", "--stat", base+"..."+head)
}

// Clone makes a shallow, blobless clone of one branch of url (a URL or a
// path) into dir, with no credential: no helper is consulted, and the
// token variables never reach the child. Blobs arrive as they are read.
func Clone(url, branch, dir string) error {
	args := []string{"-c", "credential.helper=", "-c", "core.askPass=", "clone", "-q", "--depth", "1",
		"--filter=blob:none", "--no-checkout", "--single-branch", "--branch", branch, "--", url, dir}
	cmd, done := command(RemoteTimeout, args[4:], args...)
	cmd.Env = childEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := done(cmd.Run()); err != nil {
		var te *TimeoutError
		if errors.As(err, &te) {
			return te
		}
		return fmt.Errorf("git clone %s (%s): %v: %s", url, branch, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// File is one blob of a tree.
type File struct {
	Data       []byte
	Executable bool
}

// Tree reads every file under prefix at ref, by repo-relative path. A
// symlink or submodule under prefix is refused.
func (r Repo) Tree(ref, prefix string) (map[string]File, error) {
	out, err := r.run("ls-tree", "-r", "-z", ref, "--", prefix)
	if err != nil {
		return nil, err
	}
	files := map[string]File{}
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			return nil, fmt.Errorf("git ls-tree: unexpected line %q", rec)
		}
		if f[1] != "blob" || (f[0] != "100644" && f[0] != "100755") {
			return nil, fmt.Errorf("%s at %s is a %s %s, not a regular file", path, ref, f[0], f[1])
		}
		data, err := r.run("cat-file", "blob", f[2])
		if err != nil {
			return nil, err
		}
		files[path] = File{Data: data, Executable: f[0] == "100755"}
	}
	return files, nil
}

// DeleteBranch deletes a local branch that is not checked out.
func (r Repo) DeleteBranch(name string) error {
	_, err := r.run("branch", "-q", "-D", name)
	return err
}

// Ran is one command a caller composed: its exit code and both streams.
type Ran struct {
	Code           int    `json:"code"`
	Stdout, Stderr string `json:"-"`
}

// remoteCommands talk to the remote, so they carry the token and the
// remote bound.
var remoteCommands = map[string]bool{"fetch": true, "push": true, "pull": true, "ls-remote": true, "clone": true}

// Run is git with a caller's own arguments, as the updater runs git:
// bounded, the bot's identity, the token only on a command that talks to
// the remote. A non-zero exit is an answer, not an error; a command that
// could not run or timed out is the error.
func (r Repo) Run(args ...string) (Ran, error) { return r.RunInput("", args...) }

// RunInput is Run with stdin written to the child.
func (r Repo) RunInput(stdin string, args ...string) (Ran, error) {
	if len(args) == 0 {
		return Ran{}, errors.New("git: no command")
	}
	if err := r.Faults.spent(args); err != nil {
		return Ran{}, err
	}
	cmd, done := r.child(remoteCommands[args[0]], args)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := done(cmd.Run())
	var te *TimeoutError
	var exit *exec.ExitError
	switch {
	case errors.As(err, &te):
		r.Faults.add(te.msg)
		return Ran{}, te
	case errors.As(err, &exit):
		return Ran{Code: exit.ExitCode(), Stdout: stdout.String(), Stderr: stderr.String()}, nil
	case err != nil:
		return Ran{}, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return Ran{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}
