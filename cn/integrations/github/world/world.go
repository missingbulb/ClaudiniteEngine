// Package world is the task runner's outward edge: every read and write
// the scheduler and executor make on the repository's issues, pull
// requests, workflows and the clock, each a named operation on an
// interface cn wires to the real client and the simulator wires to an
// in-memory one. A caller names the operation, never the URL; label writes
// are granular (add and remove a named label, never the label set), since
// a set-write from a stale snapshot clobbers a concurrent transition.
package world

import (
	"errors"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// ErrGone is a read of an object the repository no longer has (404, 410):
// a definitive answer, unlike any other failure.
var ErrGone = errors.New("gone")

// Issue is an issue as listed or read: pull requests included so a lister
// can drop them, with a pull request's merge instant, which is what tells
// work that landed from work abandoned.
type Issue struct {
	workitem.Issue
	PullRequest bool   `json:"pull_request,omitempty"`
	MergedAt    string `json:"merged_at,omitempty"`
	Author      string `json:"author,omitempty"`
	StateReason string `json:"state_reason,omitempty"`
}

// Query is an issues listing: a state (open, closed, all), an optional
// updated-since instant and label, and the order. An empty Sort is the
// state's own order: open oldest first, closed most recently updated
// first.
type Query struct {
	State     string
	Since     string
	Label     string
	Sort      string
	Direction string
}

// Order is the query's sort and direction, its defaults filled.
func (q Query) Order() (sort, direction string) {
	sort, direction = q.Sort, q.Direction
	if sort == "" {
		sort, direction = "created", "asc"
		if q.State == "closed" {
			sort, direction = "updated", "desc"
		}
	}
	if direction == "" {
		direction = "desc"
	}
	return sort, direction
}

// Comment is one issue comment.
type Comment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	Author    string `json:"author,omitempty"`
}

// PageSize is the page every list asks for.
const PageSize = 100

// Issues is the queue's half of the port.
type Issues interface {
	// IssuesPage is one page (PageSize) of the listing q names.
	IssuesPage(q Query, page int) ([]Issue, error)
	Issue(n int) (Issue, error)
	CreateIssue(title, body string, labels []string) (int, error)
	CloseIssue(n int, reason string) error
	ReopenIssue(n int) error
	SetIssueBody(n int, body string) error
	SetIssueTitle(n int, title string) error
	AddLabel(n int, label string) error
	// RemoveLabel succeeds when the issue does not carry the label: that
	// is the end state asked for.
	RemoveLabel(n int, label string) error
	EnsureLabels(labels []workitem.Label) error
	Comments(n int) ([]Comment, error)
	Comment(n int, body string) (int64, error)
	EditComment(id int64, body string) error
	// Permission is a login's role on the repository (admin, maintain,
	// write, triage, read, none); ErrGone when the login is no
	// collaborator.
	Permission(login string) (string, error)
}

// HasPush reports a role that may push.
func HasPush(role string) bool { return role == "admin" || role == "maintain" || role == "write" }

// Clock is the time the runner reads; the simulator and CLAUDINITE_NOW
// replace it.
type Clock interface{ Now() time.Time }

// RealClock reads the wall clock.
type RealClock struct{}

// Now is time.Now in UTC.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// FixedClock always reads the same instant.
type FixedClock time.Time

// Now is the fixed instant.
func (c FixedClock) Now() time.Time { return time.Time(c) }

// CommitRef is a commit as a history listing returns it.
type CommitRef struct {
	SHA, Message string
	// Author is the commit's GitHub login, "" when GitHub matched none.
	Author string
}

// Commit is one commit read whole: its files resolved.
type Commit struct {
	SHA, Message, Author string
	// Date is the committer's date, else the author's.
	Date  string
	Files []string
}

// Pull is a pull request as listed or read.
type Pull struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	HeadRef   string   `json:"head_ref"`
	HeadSHA   string   `json:"head_sha"`
	BaseRef   string   `json:"base_ref"`
	UpdatedAt string   `json:"updated_at"`
	MergedAt  string   `json:"merged_at"`
	Labels    []string `json:"labels"`
	NodeID    string   `json:"node_id,omitempty"`
}

// Pulls is the target's half of the port: the pull requests a run works on.
type Pulls interface {
	// Pull reads one pull request; ErrGone when there is none.
	Pull(n int) (Pull, error)
	// Mergeable is whether pull request n merges cleanly into its base;
	// nil while GitHub has not computed it.
	Mergeable(n int) (*bool, error)
	ClosePull(n int) error
	CreatePull(title, body, head, base string) (Pull, error)
}

// OpenPulls lists every open pull request, newest first, or fails: an
// unreadable page is never a shorter list.
func OpenPulls(r Repo) ([]Pull, error) {
	var all []Pull
	for page := 1; ; page++ {
		got, err := r.PullsPage("open", "created", "desc", page)
		if err != nil {
			return nil, err
		}
		all = append(all, got...)
		if len(got) < PageSize {
			return all, nil
		}
	}
}

// Branch is a branch and its tip.
type Branch struct {
	Name, SHA string
}

// Repo is the signals' half of the port: the repository's history, pull
// requests, branches and releases, read.
type Repo interface {
	// CommitsPage is one page of branch's history since an instant.
	CommitsPage(branch, since string, page int) ([]CommitRef, error)
	Commit(sha string) (Commit, error)
	// PullsPage is one page of pull requests in state, sorted.
	PullsPage(state, sort, direction string, page int) ([]Pull, error)
	PullFilesPage(n, page int) ([]string, error)
	BranchesPage(page int) ([]Branch, error)
	// Branch is ErrGone when there is no such branch.
	Branch(name string) (Branch, error)
	// TreePaths are the paths at a ref's root tree.
	TreePaths(ref string) ([]string, error)
	// LatestRelease is the newest release's tag; ErrGone when none.
	LatestRelease() (string, error)
}
