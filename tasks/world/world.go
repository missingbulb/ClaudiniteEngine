// Package world is the task runner's outward edge: every read and write
// the scheduler and executor make on the repository's issues, pull
// requests, workflows and the clock, each a named operation on an
// interface cmd/cn wires to the real client and the simulator wires to an
// in-memory one. A caller names the operation, never the URL; label writes
// are granular (add and remove a named label, never the label set), since
// a set-write from a stale snapshot clobbers a concurrent transition.
package world

import (
	"errors"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
)

// ErrGone is a read of an object the repository no longer has (404, 410):
// a definitive answer, unlike any other failure.
var ErrGone = errors.New("gone")

// Issue is an issue as listed, pull requests included so a lister can
// drop them.
type Issue struct {
	workitem.Issue
	PullRequest bool `json:"pull_request,omitempty"`
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
	// IssuesPage is one page (PageSize) of the repository's issues in
	// state open or closed, oldest first for open and most recently
	// updated first for closed, as the Node engine asks.
	IssuesPage(state string, page int) ([]Issue, error)
	Issue(n int) (workitem.Issue, error)
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
}

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
