package execute

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
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

// deliverTree lands paths, read from the checkout at root, on target's
// branch and answers the pull request it is on.
func deliverTree(sdk *SDK, root, base string, target Target, t taskspec.Task, item int, paths []string) (int, error) {
	if target.Branch == "" || target.Branch == base {
		return 0, fmt.Errorf("no branch of this task's own to deliver on (target %q)", target.Branch)
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
	if _, err := gitOK(sdk.Git, "-C", tree, "commit", "--quiet", "-m", TreeCommitMessage(t, item, paths)); err != nil {
		return 0, err
	}
	head, err := gitOK(sdk.Git, "-C", tree, "rev-parse", "HEAD")
	if err != nil {
		return 0, err
	}
	if _, err := gitOK(sdk.Git, "push", "--quiet", "--force", "origin", strings.TrimSpace(head)+":refs/heads/"+target.Branch); err != nil {
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
