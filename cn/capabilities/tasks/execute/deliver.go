package execute

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// A shell code_work has no SDK to deliver through, so a change it leaves
// in the checkout is the executor's to deliver: committed on the base
// branch's remote tip with the trailers a module worker's commitMessage
// writes, pushed to the branch the target resolved, and opened as a pull
// request unless the target names one to amend. The checkout is put back
// afterwards, since the executor's next item runs in it.

// changedPaths are the checkout's changed and untracked paths, sorted.
func changedPaths(git func(args ...string) (gitcmd.Ran, error)) ([]string, error) {
	ran, err := git("status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	if ran.Code != 0 {
		return nil, fmt.Errorf("git status exited %d: %s", ran.Code, strings.TrimSpace(ran.Stderr))
	}
	var out []string
	for _, entry := range strings.Split(ran.Stdout, "\x00") {
		if len(entry) > 3 {
			out = append(out, entry[3:])
		}
	}
	sort.Strings(out)
	return out, nil
}

// newPaths are the paths in after that before did not list.
func newPaths(before, after []string) []string {
	had := map[string]bool{}
	for _, p := range before {
		had[p] = true
	}
	var out []string
	for _, p := range after {
		if !had[p] {
			out = append(out, p)
		}
	}
	return out
}

// TreeCommitMessage is a shell code-work's commit: a subject naming the
// task and its item, the paths, and the two trailers every task commit
// carries.
func TreeCommitMessage(t taskspec.Task, item int, paths []string) string {
	trailers := workitem.TaskTrailer + ": " + t.Path()
	if expr := mergepolicy.Expression(t.Decl["automerge"]); expr != "" {
		trailers += "\n" + workitem.AutomergeTrailer + ": " + expr
	}
	return t.Path() + ": code-work for #" + strconv.Itoa(item) + "\n\n" + strings.Join(paths, "\n") + "\n\n" + trailers
}

func gitOK(git func(args ...string) (gitcmd.Ran, error), args ...string) (string, error) {
	ran, err := git(args...)
	if err != nil {
		return "", err
	}
	if ran.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", args[0], ran.Code, strings.TrimSpace(ran.Stderr))
	}
	return ran.Stdout, nil
}

// coveredPaths are the paths a delivery commits: under a task's granular
// automerge policy, those its allow terms cover file by file, naming
// each one left out; otherwise all of them. Each committed path is named.
func (c CodeWorker) coveredPaths(git func(args ...string) (gitcmd.Ran, error), t taskspec.Task, paths []string) []string {
	policy := t.Decl["automerge"]
	kept := paths
	if mergepolicy.Normalize(policy).Kind == "rules" {
		kept = nil
		for _, p := range paths {
			v := mergepolicy.Judge(policy, []mergepolicy.Entry{entryOf(git, c.Place.Root, p)}, c.Rules)
			switch {
			case len(v.Files) == 0:
				// The policy itself is refused (an unknown rule), so
				// nothing auto-merges and the pull request waits for a
				// person: the path is committed for them to judge.
				kept = append(kept, p)
			case strings.HasPrefix(v.Files[0].Verdict, "covered:"):
				kept = append(kept, p)
			default:
				c.Log("code-work changed " + p + ", which the policy " + mergepolicy.Expression(policy) + " does not cover (" + v.Files[0].Verdict + "); left out of the commit")
			}
		}
	}
	for _, p := range kept {
		c.Log("code-work's change to commit: " + p)
	}
	return kept
}

// entryOf is path's change against the checkout's HEAD.
func entryOf(git func(args ...string) (gitcmd.Ran, error), root, p string) mergepolicy.Entry {
	e := mergepolicy.Entry{File: p}
	if ran, err := git("show", "HEAD:"+p); err == nil && ran.Code == 0 {
		before := ran.Stdout
		e.Before = &before
	}
	if raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p))); err == nil {
		after := string(raw)
		e.After = &after
	}
	return e
}

// symlinkAlong names the first symlink on rel's way down from root,
// rel itself included, or "" when there is none.
func symlinkAlong(root, rel string) string {
	parts := strings.Split(rel, "/")
	for i := range parts {
		sub := strings.Join(parts[:i+1], "/")
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(sub)))
		if err != nil {
			return ""
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return sub
		}
	}
	return ""
}

// deliverTree lands paths, read from the checkout at root, on target's
// branch and answers the pull request it is on: none when the change
// leaves the base branch's tree as it is.
func deliverTree(sdk *SDK, root, base string, target Target, t taskspec.Task, item int, paths []string) (int, error) {
	if target.Branch == "" || target.Branch == base || !strings.HasPrefix(target.Branch, BranchRoot+"/") || strings.HasPrefix(target.Branch, "refs/") {
		return 0, fmt.Errorf("no branch of this task's own to deliver on (target %q, not under %s/)", target.Branch, BranchRoot)
	}
	for _, p := range paths {
		if link := symlinkAlong(root, p); link != "" {
			return 0, fmt.Errorf("code-work's change %s runs through the symlink %s, which is never delivered", p, link)
		}
	}
	if sdk.GitHub == nil && target.PR == 0 {
		return 0, errors.New("this run has no GitHub to open the pull request through")
	}
	if _, err := gitOK(sdk.Git, "fetch", "--quiet", "origin", base); err != nil {
		return 0, err
	}
	tip, err := gitOK(sdk.Git, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return 0, err
	}
	scratch, err := os.MkdirTemp("", "claudinite-deliver-")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	tree := filepath.Join(scratch, "tree")
	if _, err := gitOK(sdk.Git, "worktree", "add", "--quiet", "--detach", tree, strings.TrimSpace(tip)); err != nil {
		return 0, err
	}
	defer func() { _, _ = sdk.Git("worktree", "remove", "--force", tree) }()
	for _, p := range paths {
		if link := symlinkAlong(tree, p); link != "" {
			return 0, fmt.Errorf("the base branch holds %s as a symlink, which a delivery never writes through", link)
		}
		src, dst := filepath.Join(root, filepath.FromSlash(p)), filepath.Join(tree, filepath.FromSlash(p))
		raw, err := os.ReadFile(src)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return 0, err
			}
		case err != nil:
			return 0, err
		default:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(dst, raw, 0o644); err != nil {
				return 0, err
			}
		}
	}
	if _, err := gitOK(sdk.Git, append([]string{"-C", tree, "add", "-A", "--"}, paths...)...); err != nil {
		return 0, err
	}
	staged, err := sdk.Git("-C", tree, "diff", "--cached", "--quiet")
	if err != nil {
		return 0, err
	}
	if staged.Code == 0 {
		return 0, nil
	}
	if _, err := gitOK(sdk.Git, "-C", tree, "commit", "--quiet", "-m", TreeCommitMessage(t, item, paths)); err != nil {
		return 0, err
	}
	head, err := gitOK(sdk.Git, "-C", tree, "rev-parse", "HEAD")
	if err != nil {
		return 0, err
	}
	built, err := gitOK(sdk.Git, "-C", tree, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return 0, err
	}
	// A recompute that builds the tree the branch already holds pushes
	// nothing: a new head would discard every check already run on it.
	if target.PR != 0 {
		if _, err := gitOK(sdk.Git, "fetch", "--quiet", "origin", target.Branch); err == nil {
			if there, err := gitOK(sdk.Git, "rev-parse", "FETCH_HEAD^{tree}"); err == nil && there == built {
				return target.PR, nil
			}
		}
	}
	push := []string{"push", "--quiet", "--force", "origin", strings.TrimSpace(head) + ":refs/heads/" + target.Branch}
	if why := pushRefusal(push, base); why != "" {
		return 0, errors.New("the push is refused: " + why)
	}
	if _, err := gitOK(sdk.Git, push...); err != nil {
		return 0, err
	}
	if target.PR != 0 {
		return target.PR, nil
	}
	body := "Code-work of `" + t.Path() + "` for #" + strconv.Itoa(item) + " changed:\n\n- `" + strings.Join(paths, "`\n- `") + "`"
	p, err := sdk.GitHub.CreatePull(t.Path()+": "+summaryOf(paths), body, target.Branch, base)
	if err != nil {
		return 0, err
	}
	sdk.Opened = append(sdk.Opened, p)
	return p.Number, nil
}

func summaryOf(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	return strconv.Itoa(len(paths)) + " files"
}

// restorePaths puts paths in the checkout back as HEAD has them, removing
// those HEAD lacks.
func restorePaths(git func(args ...string) (gitcmd.Ran, error), root string, paths []string) {
	for _, p := range paths {
		if ran, err := git("cat-file", "-e", "HEAD:"+p); err == nil && ran.Code == 0 {
			_, _ = git("checkout", "HEAD", "--", p)
			continue
		}
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(p)))
	}
}
