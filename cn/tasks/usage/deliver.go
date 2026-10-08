package usage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// File is one folded file to deliver; Move is a rolling file to carry from
// its old path to its new one, bytes unchanged.
type (
	File struct{ Path, Text string }
	Move struct{ From, To string }
)

// botEnv is the identity the fold's commits are written as.
var botEnv = []string{
	"GIT_AUTHOR_NAME=claudinite[bot]", "GIT_AUTHOR_EMAIL=claudinite@users.noreply.github.com",
	"GIT_COMMITTER_NAME=claudinite[bot]", "GIT_COMMITTER_EMAIL=claudinite@users.noreply.github.com",
}

// localTimeout bounds one local git command.
const localTimeout = 2 * time.Minute

// secretEnv names variables no local git child inherits.
var secretEnv = []string{"GITHUB_TOKEN=", "GH_TOKEN=", "ACTIONS_RUNTIME_TOKEN=", "ACTIONS_ID_TOKEN_REQUEST_TOKEN="}

// RunLocal is git in root with extra environment and stdin, its stdout; a
// non-zero exit is the error.
func RunLocal(root string, env []string, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	for _, kv := range os.Environ() {
		keep := true
		for _, s := range secretEnv {
			keep = keep && !strings.HasPrefix(kv, s)
		}
		if keep {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// BaseTip is the base branch's remote tip, fetched into the checkout.
func BaseTip(h History, base string) (string, error) {
	if _, err := h.Remote("fetch", "--quiet", "origin", base); err != nil {
		return "", err
	}
	tip, err := h.Local("rev-parse", "FETCH_HEAD")
	return strings.TrimSpace(tip), err
}

// ReadAt is one file's content at a commit, nil where the path does not
// exist there.
func ReadAt(h History, sha, path string) *string {
	text, err := h.Local("show", sha+":"+path)
	if err != nil {
		return nil
	}
	return &text
}

// ReadRollingAt is a rolling file's prior state: at path, or at legacy
// where it has not moved yet, with the move that carries it.
func ReadRollingAt(h History, sha, path, legacy string) (*string, []Move) {
	text := ReadAt(h, sha, path)
	if text != nil || legacy == "" {
		return text, nil
	}
	if old := ReadAt(h, sha, legacy); old != nil {
		return old, []Move{{From: legacy, To: path}}
	}
	return nil, nil
}

func blobAt(h History, sha, path string) string {
	out, err := h.Local("rev-parse", "--verify", "--quiet", sha+":"+path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CommitFiles writes files as a commit on baseSha through a scratch index,
// so the checkout's own index and tree are untouched. Each move whose
// source is on the base and whose target is not lands first, as its own
// commit carrying the blob unchanged.
func CommitFiles(root, baseSha string, files []File, moves []Move, message string, moveMessage func([]string) string) (string, error) {
	scratch, err := os.MkdirTemp("", "claudinite-fold-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	env := append(append([]string{}, botEnv...), "GIT_INDEX_FILE="+filepath.Join(scratch, "index"))
	plumb := func(stdin string, args ...string) (string, error) {
		out, err := RunLocal(root, env, stdin, args...)
		return strings.TrimSpace(out), err
	}
	h := History{Local: func(args ...string) (string, error) { return RunLocal(root, nil, "", args...) }}
	commitTree := func(parent, msg string) (string, error) {
		tree, err := plumb("", "write-tree")
		if err != nil {
			return "", err
		}
		return plumb("", "commit-tree", tree, "-p", parent, "-m", msg)
	}
	if _, err := plumb("", "read-tree", baseSha); err != nil {
		return "", err
	}
	parent := baseSha
	var moved []string
	for _, m := range moves {
		blob := blobAt(h, baseSha, m.From)
		if blob == "" || blobAt(h, baseSha, m.To) != "" {
			continue
		}
		if _, err := plumb("", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+m.To); err != nil {
			return "", err
		}
		if _, err := plumb("", "update-index", "--force-remove", m.From); err != nil {
			return "", err
		}
		moved = append(moved, m.From+" -> "+m.To)
	}
	if len(moved) > 0 {
		if parent, err = commitTree(parent, moveMessage(moved)); err != nil {
			return "", err
		}
	}
	for _, f := range files {
		blob, err := plumb(f.Text, "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		if _, err := plumb("", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+f.Path); err != nil {
			return "", err
		}
	}
	return commitTree(parent, message)
}

// Change is what the folds hand the delivery.
type Change struct {
	Files                []File
	Moves                []Move
	Subject, Title, Body string
}

// Delivered is where a change landed: its branch, its pull request (0 when
// the opening answered none) and whether that was one the executor named.
type Delivered struct {
	Branch string
	Number int
	Reused bool
}

// Delivery lands a change on the branch the executor resolved: Branch is
// required, PR names an open pull request to amend. Trailers are the
// task's, appended to each commit message; OpenPr opens a pull request and
// answers its number.
type Delivery struct {
	Root, Base string
	Branch     string
	PR         int
	History    History
	Trailers   string
	OpenPr     func(title, body, head, base string) (int, error)
}

func commitMessage(subject, body, trailers string) string {
	var parts []string
	for _, p := range []string{subject, body, trailers} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}

// Deliver commits the change on the base's remote tip, force-pushes it to
// the target branch and opens the pull request unless one was named. A
// commit whose tree the branch already holds pushes nothing.
func (d Delivery) Deliver(c Change) (Delivered, error) {
	if d.Branch == "" {
		return Delivered{}, errors.New("no branch to deliver on — the executor resolves it and hands it in as the target branch")
	}
	if d.Branch == d.Base {
		return Delivered{}, fmt.Errorf("refusing to force-push to %s, the base branch — a delivery lands on the task's own branch", d.Base)
	}
	baseSha, err := BaseTip(d.History, d.Base)
	if err != nil {
		return Delivered{}, err
	}
	commit, err := CommitFiles(d.Root, baseSha, c.Files, c.Moves, commitMessage(c.Subject, "", d.Trailers), func(moved []string) string {
		what := "rolling files"
		if len(moved) == 1 {
			what = "a rolling file"
		}
		return commitMessage("Move "+what+" to their new home, content unchanged", strings.Join(moved, "\n"), d.Trailers)
	})
	if err != nil {
		return Delivered{}, err
	}
	if d.PR != 0 && d.holdsTree(commit) {
		return Delivered{Branch: d.Branch, Number: d.PR, Reused: true}, nil
	}
	if _, err := d.History.Remote("push", "--quiet", "--force", "origin", commit+":refs/heads/"+d.Branch); err != nil {
		return Delivered{}, err
	}
	if d.PR != 0 {
		return Delivered{Branch: d.Branch, Number: d.PR, Reused: true}, nil
	}
	n, err := d.OpenPr(c.Title, c.Body, d.Branch, d.Base)
	if err != nil {
		return Delivered{}, err
	}
	return Delivered{Branch: d.Branch, Number: n}, nil
}

// holdsTree reports the target branch's remote tip carrying commit's tree.
func (d Delivery) holdsTree(commit string) bool {
	if _, err := d.History.Remote("fetch", "--quiet", "origin", d.Branch); err != nil {
		return false
	}
	there, err := d.History.Local("rev-parse", "FETCH_HEAD^{tree}")
	if err != nil {
		return false
	}
	built, err := d.History.Local("rev-parse", commit+"^{tree}")
	return err == nil && strings.TrimSpace(there) == strings.TrimSpace(built)
}

// Half is one half of the fold: its name and the fold that hands back its
// files, or none when its recompute is byte-identical.
type Half struct {
	Name string
	Fold func() (Folded, error)
}

// Folded is what one half hands the delivery: its files and moves, the
// summary it logs and the report that rides the pull request body.
type Folded struct {
	Files   []File
	Moves   []Move
	Summary string
	Report  []string
}

func mergeFiles(into []File, from []File) []File {
	for _, f := range from {
		replaced := false
		for i := range into {
			if into[i].Path == f.Path {
				into[i], replaced = f, true
			}
		}
		if !replaced {
			into = append(into, f)
		}
	}
	return into
}

func mergeMoves(into []Move, from []Move) []Move {
	for _, m := range from {
		replaced := false
		for i := range into {
			if into[i].From == m.From {
				into[i], replaced = m, true
			}
		}
		if !replaced {
			into = append(into, m)
		}
	}
	return into
}

// deliveryBody is the fold pull request's body.
func deliveryBody(files []File, reports []string) string {
	lines := []string{"Regenerated the repo's rolling usage records:", ""}
	for _, f := range files {
		lines = append(lines, "- `"+f.Path+"`")
	}
	lines = append(lines, "",
		"`"+UsagePath+"` folds what the repo's sessions did - its captured conversation",
		"logs, its workflow run listings, the queue's closed work items, its merged pull",
		"requests and its git history. Hour rows cover the last three days; day rows are",
		"recomputed from scratch every run; the run, queue and merged-PR rows and the week",
		"rows are appended once, past their own watermarks.",
		"",
		"`"+TasksUsagePath+"` folds what the machinery itself cost - per workflow the runs,",
		"jobs, billed minutes and, only where this pack's config carries `actionsMinuteRate`,",
		"the spend they imply; per run its API calls and wall time per phase; per task its",
		"outcomes, parks and latency samples. Every tier is appended once past its own watermark.",
		"",
		"A file whose recompute differs only in its `generated` stamp is left out, and a fold",
		"where neither moved opens no PR at all. Machine-written - never hand-edit either;",
		"each fold starts from the last, so a lost copy is lost history.")
	return strings.Join(append(lines, reports...), "\n")
}

// DeliverFolds runs every half and lands whatever changed on one pull
// request. A half that fails costs only its own file: the rest still
// deliver, and the run then fails naming every broken half.
func DeliverFolds(halves []Half, deliver func(Change) (Delivered, error), log func(string)) (*Delivered, error) {
	var files []File
	var moves []Move
	var failures, summaries, reports []string
	for _, h := range halves {
		out, err := h.Fold()
		if err != nil {
			log("the " + h.Name + " half failed - its file is unchanged this run: " + err.Error())
			failures = append(failures, h.Name+": "+err.Error())
			continue
		}
		files, moves = mergeFiles(files, out.Files), mergeMoves(moves, out.Moves)
		summaries = append(summaries, h.Name+": "+out.Summary)
		if len(out.Report) > 0 {
			reports = append(append(reports, ""), out.Report...)
		}
	}
	for _, line := range append(summaries, reports...) {
		log(line)
	}
	var landed *Delivered
	if len(files) > 0 {
		pr, err := deliver(Change{Files: files, Moves: moves, Subject: "Claudinite: fold usage", Title: "Claudinite: usage fold",
			Body: deliveryBody(files, reports)})
		if err != nil {
			return nil, err
		}
		landed = &pr
		verb, where := "opened", "on "+pr.Branch
		if pr.Reused {
			verb = "updated"
		}
		if pr.Number != 0 {
			where = "#" + strconv.Itoa(pr.Number)
		}
		log(verb + " PR " + where)
	} else if len(failures) == 0 {
		log("every recompute is byte-identical - nothing to deliver")
	}
	if len(failures) > 0 {
		return landed, errors.New("usage fold: " + strings.Join(failures, "; "))
	}
	return landed, nil
}
