// Package gitcmd runs the git binary for the updater: branch, commit,
// push, read a file at a ref, and list what a branch changed. Commits are
// made as github-actions[bot], the identity whose pin changes CI accepts.
// No token reaches a git child: pushing uses the credentials the checkout
// configured.
package gitcmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Bot is the identity update commits carry.
const (
	BotName  = "github-actions[bot]"
	BotEmail = "41898282+github-actions[bot]@users.noreply.github.com"
)

// Repo is a working tree.
type Repo struct{ Dir string }

// secretEnv names variables no git child may inherit.
var secretEnv = []string{"GITHUB_TOKEN", "GH_TOKEN", "ACTIONS_RUNTIME_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "NODE_AUTH_TOKEN"}

func childEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		keep := true
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

func (r Repo) run(args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-c", "user.name=" + BotName, "-c", "user.email=" + BotEmail}, args...)...)
	cmd.Dir = r.Dir
	cmd.Env = childEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (r Repo) line(args ...string) (string, error) {
	out, err := r.run(args...)
	return strings.TrimSpace(string(out)), err
}

// Head is the checked-out commit.
func (r Repo) Head() (string, error) { return r.line("rev-parse", "HEAD") }

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
	_, err := r.run("push", "-q", "--force", remote, "refs/heads/"+branch+":refs/heads/"+branch)
	return err
}

// DeleteRemoteBranch deletes branch on remote; an already absent branch is
// not an error.
func (r Repo) DeleteRemoteBranch(remote, branch string) error {
	out, err := r.run("ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil
	}
	_, err = r.run("push", "-q", remote, "--delete", "refs/heads/"+branch)
	return err
}

// Fetch fetches refspecs from remote.
func (r Repo) Fetch(remote string, refspecs ...string) error {
	_, err := r.run(append([]string{"fetch", "-q", remote}, refspecs...)...)
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
