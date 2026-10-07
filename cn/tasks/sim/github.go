// Package sim is the task runner's fake world: an in-memory repository
// and clock the real scheduler and executor run against, so the
// mechanism's claims run as tests asserting on the store the engine wrote,
// never on an answer a scenario states. It models the limitations the
// mechanism is built around: comment ids are server-assigned and strictly
// increasing while comment timestamps have one-second granularity (why a
// claim is arbitrated by id), label writes are granular and a swap is two
// calls with no compare-and-set, and a list is paged. Faults are off by
// default and switched on per scenario.
package sim

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// ErrRateLimited is the platform refusing a healthy caller.
var ErrRateLimited = errors.New("403 API rate limit exceeded")

// ErrUnreadable is a read the platform answers 500.
var ErrUnreadable = errors.New("500 server error")

// Clock is a virtual clock a scenario advances.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// NewClock starts at t.
func NewClock(t time.Time) *Clock { return &Clock{t: t.UTC()} }

// Now is the virtual instant.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t.UTC()
	c.mu.Unlock()
}

// StoredIssue is an issue in the store, comments with it.
type StoredIssue struct {
	workitem.Issue
	StateReason string
	PullRequest bool
	MergedAt    string
	Author      string
	Comments    []world.Comment
}

func (i *StoredIssue) view() world.Issue {
	c := i.Issue
	c.Labels = append(workitem.LabelList{}, i.Labels...)
	return world.Issue{Issue: c, PullRequest: i.PullRequest, MergedAt: i.MergedAt, Author: i.Author, StateReason: i.StateReason}
}

// Faults are the platform's misbehaviours a scenario turns on.
type Faults struct {
	// RateLimited is how many calls remain that answer 403.
	RateLimited int
	// Hidden issues a list read cannot see yet.
	Hidden map[int]bool
	// TearNextSwap fails the next AddLabel after a RemoveLabel landed.
	TearNextSwap bool
	// Unreadable issues whose direct read answers 500.
	Unreadable map[int]bool
}

// GitHub is the in-memory repository.
type GitHub struct {
	mu        sync.Mutex
	Clock     *Clock
	Repo      string
	issues    []*StoredIssue
	labelDefs map[string]workitem.Label
	Faults    Faults
	Calls     []string
	// Roles are collaborators' permissions; a login absent is none.
	Roles      map[string]string
	commentSeq int64
	removed    bool
}

// NewGitHub is an empty repository on clock.
func NewGitHub(clock *Clock) *GitHub {
	return &GitHub{Clock: clock, Repo: "o/r", labelDefs: map[string]workitem.Label{}, commentSeq: 100, Roles: map[string]string{},
		Faults: Faults{Hidden: map[int]bool{}, Unreadable: map[int]bool{}}}
}

func (g *GitHub) now() string { return calendarISO(g.Clock.Now()) }

func calendarISO(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func secondISO(t time.Time) string { return calendarISO(t.Truncate(time.Second)) }

func (g *GitHub) charge(call string) error {
	g.Calls = append(g.Calls, call)
	if g.Faults.RateLimited > 0 {
		g.Faults.RateLimited--
		return ErrRateLimited
	}
	return nil
}

func (g *GitHub) find(n int) *StoredIssue {
	for _, i := range g.issues {
		if i.Number == n {
			return i
		}
	}
	return nil
}

func (g *GitHub) touch(i *StoredIssue) { i.UpdatedAt = g.now() }

// Seed adds an issue as it stands, numbering it when Number is zero.
func (g *GitHub) Seed(i StoredIssue) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i.Number == 0 {
		i.Number = g.nextNumber()
	}
	if i.State == "" {
		i.State = "open"
	}
	if i.CreatedAt == "" {
		i.CreatedAt = g.now()
	}
	if i.UpdatedAt == "" {
		i.UpdatedAt = i.CreatedAt
	}
	c := i
	g.issues = append(g.issues, &c)
	return c.Number
}

func (g *GitHub) nextNumber() int {
	n := 0
	for _, i := range g.issues {
		if i.Number > n {
			n = i.Number
		}
	}
	return n + 1
}

// Get is a copy of issue n as stored, comments included; false when absent.
func (g *GitHub) Get(n int) (StoredIssue, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.find(n)
	if i == nil {
		return StoredIssue{}, false
	}
	c := *i
	c.Labels = append(workitem.LabelList{}, i.Labels...)
	c.Comments = append([]world.Comment{}, i.Comments...)
	return c, true
}

// All is every issue, in number order.
func (g *GitHub) All() []StoredIssue {
	g.mu.Lock()
	ns := make([]int, 0, len(g.issues))
	for _, i := range g.issues {
		ns = append(ns, i.Number)
	}
	g.mu.Unlock()
	sort.Ints(ns)
	out := make([]StoredIssue, 0, len(ns))
	for _, n := range ns {
		i, _ := g.Get(n)
		out = append(out, i)
	}
	return out
}

var _ world.Issues = (*GitHub)(nil)

// IssuesPage lists issues as the issues list API does: by state, since
// (on updated_at) and label, sorted, PageSize to a page.
func (g *GitHub) IssuesPage(q world.Query, page int) ([]world.Issue, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.charge(fmt.Sprintf("GET issues?state=%s&since=%s&labels=%s&page=%d", q.State, q.Since, q.Label, page)); err != nil {
		return nil, err
	}
	var all []*StoredIssue
	for _, i := range g.issues {
		if (q.State != "all" && i.State != q.State) || g.Faults.Hidden[i.Number] {
			continue
		}
		if q.Since != "" && i.UpdatedAt < q.Since {
			continue
		}
		if q.Label != "" && !i.HasLabel(q.Label) {
			continue
		}
		all = append(all, i)
	}
	key, dir := q.Order()
	at := func(i *StoredIssue) string {
		if key == "updated" {
			return i.UpdatedAt
		}
		return fmt.Sprintf("%s#%09d", i.CreatedAt, i.Number)
	}
	sort.SliceStable(all, func(a, b int) bool {
		if dir == "asc" {
			return at(all[a]) < at(all[b])
		}
		return at(all[a]) > at(all[b])
	})
	from := (page - 1) * world.PageSize
	out := []world.Issue{}
	for k := from; k < len(all) && k < from+world.PageSize; k++ {
		out = append(out, all[k].view())
	}
	return out, nil
}

// Permission is a login's role; ErrGone for a stranger.
func (g *GitHub) Permission(login string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.charge("GET collaborators/" + login + "/permission"); err != nil {
		return "", err
	}
	if r, ok := g.Roles[login]; ok {
		return r, nil
	}
	return "", world.ErrGone
}

// Issue reads one issue.
func (g *GitHub) Issue(n int) (world.Issue, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.charge(fmt.Sprintf("GET issues/%d", n)); err != nil {
		return world.Issue{}, err
	}
	if g.Faults.Unreadable[n] {
		return world.Issue{}, ErrUnreadable
	}
	i := g.find(n)
	if i == nil {
		return world.Issue{}, world.ErrGone
	}
	return i.view(), nil
}

// CreateIssue files an issue.
func (g *GitHub) CreateIssue(title, body string, labels []string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.charge("POST issues"); err != nil {
		return 0, err
	}
	for _, l := range labels {
		if _, ok := g.labelDefs[l]; !ok {
			g.labelDefs[l] = workitem.Label{Name: l}
		}
	}
	i := &StoredIssue{Issue: workitem.Issue{Number: g.nextNumber(), Title: title, Body: body, State: "open",
		Labels: append(workitem.LabelList{}, labels...), CreatedAt: g.now(), UpdatedAt: g.now()}}
	g.issues = append(g.issues, i)
	return i.Number, nil
}

func (g *GitHub) mutate(call string, n int, f func(*StoredIssue) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.charge(call); err != nil {
		return err
	}
	i := g.find(n)
	if i == nil {
		return world.ErrGone
	}
	if err := f(i); err != nil {
		return err
	}
	g.touch(i)
	return nil
}

// CloseIssue closes an issue with a reason.
func (g *GitHub) CloseIssue(n int, reason string) error {
	return g.mutate(fmt.Sprintf("PATCH issues/%d close", n), n, func(i *StoredIssue) error {
		i.State, i.StateReason, i.ClosedAt = "closed", reason, g.now()
		return nil
	})
}

// ReopenIssue reopens an issue.
func (g *GitHub) ReopenIssue(n int) error {
	return g.mutate(fmt.Sprintf("PATCH issues/%d reopen", n), n, func(i *StoredIssue) error {
		i.State, i.StateReason, i.ClosedAt = "open", "", ""
		return nil
	})
}

// SetIssueBody replaces an issue's body.
func (g *GitHub) SetIssueBody(n int, body string) error {
	return g.mutate(fmt.Sprintf("PATCH issues/%d body", n), n, func(i *StoredIssue) error { i.Body = body; return nil })
}

// SetIssueTitle replaces an issue's title.
func (g *GitHub) SetIssueTitle(n int, title string) error {
	return g.mutate(fmt.Sprintf("PATCH issues/%d title", n), n, func(i *StoredIssue) error { i.Title = title; return nil })
}

// AddLabel adds one label.
func (g *GitHub) AddLabel(n int, label string) error {
	return g.mutate(fmt.Sprintf("POST issues/%d/labels %s", n, label), n, func(i *StoredIssue) error {
		if g.Faults.TearNextSwap && g.removed {
			g.Faults.TearNextSwap, g.removed = false, false
			return errors.New("502 the add of a torn swap")
		}
		for _, l := range i.Labels {
			if l == label {
				return nil
			}
		}
		i.Labels = append(i.Labels, label)
		return nil
	})
}

// RemoveLabel removes one label; absent is success.
func (g *GitHub) RemoveLabel(n int, label string) error {
	return g.mutate(fmt.Sprintf("DELETE issues/%d/labels/%s", n, label), n, func(i *StoredIssue) error {
		out := i.Labels[:0:0]
		for _, l := range i.Labels {
			if l == label {
				g.removed = true
				continue
			}
			out = append(out, l)
		}
		i.Labels = out
		return nil
	})
}

// EnsureLabels defines every label the repository lacks.
func (g *GitHub) EnsureLabels(labels []workitem.Label) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, l := range labels {
		if _, ok := g.labelDefs[l.Name]; ok {
			continue
		}
		if err := g.charge("POST labels " + l.Name); err != nil {
			return err
		}
		g.labelDefs[l.Name] = l
	}
	return nil
}

// LabelDefined reports whether the repository defines a label.
func (g *GitHub) LabelDefined(name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.labelDefs[name]
	return ok
}

// Comments lists an issue's comments, oldest first.
func (g *GitHub) Comments(n int) ([]world.Comment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.charge(fmt.Sprintf("GET issues/%d/comments", n)); err != nil {
		return nil, err
	}
	i := g.find(n)
	if i == nil {
		return nil, world.ErrGone
	}
	return append([]world.Comment{}, i.Comments...), nil
}

// Comment posts a comment and returns its id.
func (g *GitHub) Comment(n int, body string) (int64, error) {
	var id int64
	err := g.mutate(fmt.Sprintf("POST issues/%d/comments", n), n, func(i *StoredIssue) error {
		g.commentSeq++
		id = g.commentSeq
		i.Comments = append(i.Comments, world.Comment{ID: id, Body: body, CreatedAt: secondISO(g.Clock.Now()), Author: "github-actions[bot]"})
		return nil
	})
	return id, err
}

// CommentAs posts a comment by a named author, as a person or another
// session would.
func (g *GitHub) CommentAs(n int, author, body string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.find(n)
	if i == nil {
		return 0
	}
	g.commentSeq++
	i.Comments = append(i.Comments, world.Comment{ID: g.commentSeq, Body: body, CreatedAt: secondISO(g.Clock.Now()), Author: author})
	g.touch(i)
	return g.commentSeq
}

// EditComment replaces a comment's body.
func (g *GitHub) EditComment(id int64, body string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.charge(fmt.Sprintf("PATCH comments/%d", id)); err != nil {
		return err
	}
	for _, i := range g.issues {
		for k := range i.Comments {
			if i.Comments[k].ID == id {
				i.Comments[k].Body = body
				g.touch(i)
				return nil
			}
		}
	}
	return world.ErrGone
}

// MarkMerged closes pull request n as merged now.
func (g *GitHub) MarkMerged(n int) error {
	return g.mutate(fmt.Sprintf("PUT pulls/%d/merge", n), n, func(i *StoredIssue) error {
		i.State, i.MergedAt, i.ClosedAt = "closed", g.now(), g.now()
		return nil
	})
}
