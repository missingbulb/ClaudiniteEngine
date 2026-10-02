package sim

import (
	"sort"
	"sync"

	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
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
}

var _ world.Repo = (*Repo)(nil)

// NewRepo is an empty history.
func NewRepo() *Repo { return &Repo{Trees: map[string][]string{}} }

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
