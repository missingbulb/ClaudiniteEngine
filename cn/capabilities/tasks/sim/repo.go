package sim

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/world"
)

// Repo is the in-memory repository history the signals read: commits on
// the default branch, pull requests, branches, a release and the
// conversation-log tree. A scenario seeds it; nothing here writes back.
type Repo struct {
	mu       sync.Mutex
	Commits  []world.Commit
	Pulls    []world.Pull
	Branches []world.Branch
	Release  string
	Trees    map[string][]string
	// Unreadable fails every read, a 500 the platform answers.
	Unreadable bool

	// Heads are commits off the default branch, a pull request's head
	// among them, which Commit reads and the history does not list.
	Heads map[string]world.Commit
	// MergeableReads answers successive Mergeable reads per pull request,
	// the last answer repeating; absent reads true.
	MergeableReads map[int][]*bool
	// Runs are the workflow runs per head sha.
	Runs map[string][]land.Run
	// Workflows are the workflow files every ref carries.
	Workflows []land.WorkflowFile
	// Dispatchable names the workflows a dispatch is accepted for.
	Dispatchable map[string]bool
	// HeldOnOpen names the workflows whose pull_request run CreatePull
	// starts held at action_required, as GitHub holds a job-token PR's.
	HeldOnOpen []string
	// Approved is what a run approved out of action_required concludes;
	// "" leaves it queued.
	Approved string
	// ApproveRefused refuses every approval with this status.
	ApproveRefused int
	// Protected is the default branch's protected flag; nil is unreadable.
	Protected *bool
	// Rules are the ruleset rule types on the default branch.
	Rules []string
	// MergeRefused refuses every merge with this status.
	MergeRefused int
	// Log records every write, in order.
	Log []string

	runID int64
}

var (
	_ world.Repo  = (*Repo)(nil)
	_ world.Pulls = (*Repo)(nil)
	_ land.API    = (*Repo)(nil)
)

// NewRepo is an empty history.
func NewRepo() *Repo {
	return &Repo{Trees: map[string][]string{}, Heads: map[string]world.Commit{}, MergeableReads: map[int][]*bool{},
		Runs: map[string][]land.Run{}, Dispatchable: map[string]bool{}}
}

func (r *Repo) logf(format string, a ...any) { r.Log = append(r.Log, fmt.Sprintf(format, a...)) }

func (r *Repo) pull(n int) *world.Pull {
	for i := range r.Pulls {
		if r.Pulls[i].Number == n {
			return &r.Pulls[i]
		}
	}
	return nil
}

// Pull reads one pull request.
func (r *Repo) Pull(n int) (world.Pull, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Unreadable {
		return world.Pull{}, ErrUnreadable
	}
	if p := r.pull(n); p != nil {
		return *p, nil
	}
	return world.Pull{}, world.ErrGone
}

// Mergeable answers the next scripted read.
func (r *Repo) Mergeable(n int) (*bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Unreadable {
		return nil, ErrUnreadable
	}
	if r.pull(n) == nil {
		return nil, world.ErrGone
	}
	reads := r.MergeableReads[n]
	if len(reads) == 0 {
		yes := true
		return &yes, nil
	}
	v := reads[0]
	if len(reads) > 1 {
		r.MergeableReads[n] = reads[1:]
	}
	return v, nil
}

// ClosePull closes a pull request.
func (r *Repo) ClosePull(n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.pull(n)
	if p == nil {
		return world.ErrGone
	}
	p.State = "closed"
	r.logf("close #%d", n)
	return nil
}

// CreatePull opens a pull request numbered after every existing one.
func (r *Repo) CreatePull(title, body, head, base string) (world.Pull, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 100
	for _, p := range r.Pulls {
		n = max(n, p.Number+1)
	}
	p := world.Pull{Number: n, Title: title, Body: body, State: "open", HeadRef: head, HeadSHA: "sha-" + head, BaseRef: base, NodeID: fmt.Sprintf("PR_%d", n)}
	r.Pulls = append(r.Pulls, p)
	for _, name := range r.HeldOnOpen {
		r.runID++
		r.Runs[p.HeadSHA] = append(r.Runs[p.HeadSHA], land.Run{ID: r.runID, Name: name, Event: "pull_request", Status: "completed", Conclusion: "action_required"})
	}
	r.logf("open #%d %s", n, head)
	return p, nil
}

// WorkflowFiles are the scripted workflows.
func (r *Repo) WorkflowFiles(string) ([]land.WorkflowFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]land.WorkflowFile{}, r.Workflows...), nil
}

// DispatchWorkflow accepts a dispatchable workflow.
func (r *Repo) DispatchWorkflow(name, ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Dispatchable[name] {
		return &land.StatusError{Status: 403}
	}
	r.logf("dispatch %s %s", name, ref)
	return nil
}

// BranchProtected is the scripted flag.
func (r *Repo) BranchProtected(string) (*bool, error) { return r.Protected, nil }

// BranchRules are the scripted rules.
func (r *Repo) BranchRules(string) ([]string, error) { return r.Rules, nil }

// RunsForSHA are the scripted runs on a head.
func (r *Repo) RunsForSHA(sha string) ([]land.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]land.Run{}, r.Runs[sha]...), nil
}

// ApproveRun releases a run held at action_required.
func (r *Repo) ApproveRun(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ApproveRefused != 0 {
		return &land.StatusError{Status: r.ApproveRefused, Message: "refused"}
	}
	for sha, runs := range r.Runs {
		for i := range runs {
			if runs[i].ID != id {
				continue
			}
			if runs[i].Conclusion != "action_required" {
				return &land.StatusError{Status: 403, Message: "This run is not waiting for approval"}
			}
			runs[i].Status, runs[i].Conclusion = "queued", ""
			if r.Approved != "" {
				runs[i].Status, runs[i].Conclusion = "completed", r.Approved
			}
			r.Runs[sha] = runs
			r.logf("approve %d", id)
			return nil
		}
	}
	return &land.StatusError{Status: 404, Message: "Not Found"}
}

// MergePull merges a pull request at its pinned head.
func (r *Repo) MergePull(m land.Merge) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.MergeRefused != 0 {
		return &land.StatusError{Status: r.MergeRefused, Message: "refused"}
	}
	p := r.pull(m.Number)
	if p == nil {
		return world.ErrGone
	}
	if m.SHA != "" && p.HeadSHA != m.SHA {
		return &land.StatusError{Status: 409, Message: "Head branch was modified"}
	}
	p.State, p.MergedAt = "closed", "merged"
	r.logf("merge #%d %s %s", m.Number, m.SHA, strings.TrimSpace(m.Message))
	return nil
}

// EnableAutoMerge arms a pull request.
func (r *Repo) EnableAutoMerge(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logf("arm %s", nodeID)
	return nil
}

// DeleteBranch removes a branch.
func (r *Repo) DeleteBranch(ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logf("delete %s", ref)
	return nil
}

// AddCommit lands a commit on the default branch.
func (r *Repo) AddCommit(c world.Commit) {
	r.mu.Lock()
	r.Commits = append(r.Commits, c)
	r.mu.Unlock()
}

func page[T any](all []T, n int) []T {
	from := (n - 1) * world.PageSize
	if from >= len(all) {
		return []T{}
	}
	to := min(from+world.PageSize, len(all))
	return append([]T{}, all[from:to]...)
}

// CommitsPage lists the default branch's commits since an instant,
// newest first.
func (r *Repo) CommitsPage(_ string, since string, n int) ([]world.CommitRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Unreadable {
		return nil, ErrUnreadable
	}
	var in []world.Commit
	for _, c := range r.Commits {
		if since == "" || c.Date >= since {
			in = append(in, c)
		}
	}
	sort.SliceStable(in, func(a, b int) bool { return in[a].Date > in[b].Date })
	var refs []world.CommitRef
	for _, c := range page(in, n) {
		refs = append(refs, world.CommitRef{SHA: c.SHA, Message: c.Message, Author: c.Author})
	}
	return refs, nil
}

// Commit reads one commit.
func (r *Repo) Commit(sha string) (world.Commit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Unreadable {
		return world.Commit{}, ErrUnreadable
	}
	for _, c := range r.Commits {
		if c.SHA == sha {
			return c, nil
		}
	}
	if c, ok := r.Heads[sha]; ok {
		return c, nil
	}
	return world.Commit{}, world.ErrGone
}

// PullsPage lists pull requests in a state, most recently updated first.
func (r *Repo) PullsPage(state, _, _ string, n int) ([]world.Pull, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Unreadable {
		return nil, ErrUnreadable
	}
	var in []world.Pull
	for _, p := range r.Pulls {
		if p.State == state {
			in = append(in, p)
		}
	}
	sort.SliceStable(in, func(a, b int) bool { return in[a].UpdatedAt > in[b].UpdatedAt })
	return page(in, n), nil
}

// PullFilesPage is a pull request's files: none recorded here.
func (r *Repo) PullFilesPage(int, int) ([]string, error) { return []string{}, nil }

// BranchesPage lists the branches.
func (r *Repo) BranchesPage(n int) ([]world.Branch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return page(r.Branches, n), nil
}

// Branch reads one branch.
func (r *Repo) Branch(name string) (world.Branch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.Branches {
		if b.Name == name {
			return b, nil
		}
	}
	return world.Branch{}, world.ErrGone
}

// TreePaths lists a ref's root tree.
func (r *Repo) TreePaths(ref string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.Trees[ref]...), nil
}

// LatestRelease is the newest release's tag.
func (r *Repo) LatestRelease() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Release == "" {
		return "", world.ErrGone
	}
	return r.Release, nil
}
