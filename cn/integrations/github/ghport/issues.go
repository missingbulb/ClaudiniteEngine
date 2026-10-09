// Package ghport is the task runner's port over GitHub's REST API: the
// issues and repository reads the scheduler and executor make, the pull
// requests a run works on, the landing lane's calls and what the SDK's
// actions reach, each one call (or one page of one).
package ghport

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/world"
)

// World is the port over one repository's client.
type World struct{ c *githubapi.Client }

// New is the port over c.
func New(c *githubapi.Client) World { return World{c} }

var (
	_ world.Issues = World{}
	_ world.Repo   = World{}
)

func (g World) path(format string, a ...any) string {
	return "/repos/" + g.c.Repo + fmt.Sprintf(format, a...)
}

// gone maps a 404 or 410 onto world.ErrGone, the one definitive answer.
func gone(err error) error {
	if s := githubapi.StatusOf(err); s == 404 || s == 410 {
		return fmt.Errorf("%w: %v", world.ErrGone, err)
	}
	return err
}

type wireLogin struct {
	Login string `json:"login"`
}

type wireIssue struct {
	workitem.Issue
	StateReason string    `json:"state_reason"`
	User        wireLogin `json:"user"`
	PullRequest *struct {
		MergedAt string `json:"merged_at"`
	} `json:"pull_request"`
}

func (w wireIssue) issue() world.Issue {
	out := world.Issue{Issue: w.Issue, Author: w.User.Login, StateReason: w.StateReason}
	if w.PullRequest != nil {
		out.PullRequest, out.MergedAt = true, w.PullRequest.MergedAt
	}
	return out
}

func (g World) IssuesPage(q world.Query, page int) ([]world.Issue, error) {
	sort, dir := q.Order()
	v := url.Values{"state": {q.State}, "sort": {sort}, "direction": {dir}, "per_page": {strconv.Itoa(world.PageSize)}, "page": {strconv.Itoa(page)}}
	if q.Since != "" {
		v.Set("since", q.Since)
	}
	if q.Label != "" {
		v.Set("labels", q.Label)
	}
	var raw []wireIssue
	if err := g.c.Call("GET", g.path("/issues?%s", v.Encode()), nil, &raw); err != nil {
		return nil, err
	}
	out := make([]world.Issue, len(raw))
	for i, w := range raw {
		out[i] = w.issue()
	}
	return out, nil
}

func (g World) Issue(n int) (world.Issue, error) {
	var w wireIssue
	if err := g.c.Call("GET", g.path("/issues/%d", n), nil, &w); err != nil {
		return world.Issue{}, gone(err)
	}
	return w.issue(), nil
}

func (g World) CreateIssue(title, body string, labels []string) (int, error) {
	if err := workitem.RefuseUnapproved(labels...); err != nil {
		return 0, err
	}
	var out struct {
		Number int `json:"number"`
	}
	err := g.c.Call("POST", g.path("/issues"), map[string]any{"title": title, "body": body, "labels": labels}, &out)
	return out.Number, err
}

func (g World) patch(n int, fields map[string]any) error {
	return gone(g.c.Call("PATCH", g.path("/issues/%d", n), fields, nil))
}

func (g World) CloseIssue(n int, reason string) error {
	return g.patch(n, map[string]any{"state": "closed", "state_reason": reason})
}
func (g World) ReopenIssue(n int) error { return g.patch(n, map[string]any{"state": "open"}) }
func (g World) SetIssueBody(n int, body string) error {
	return g.patch(n, map[string]any{"body": body})
}
func (g World) SetIssueTitle(n int, title string) error {
	return g.patch(n, map[string]any{"title": title})
}

func (g World) AddLabel(n int, label string) error {
	if err := workitem.RefuseUnapproved(label); err != nil {
		return err
	}
	return gone(g.c.Call("POST", g.path("/issues/%d/labels", n), map[string]any{"labels": []string{label}}, nil))
}

func (g World) RemoveLabel(n int, label string) error {
	err := g.c.Call("DELETE", g.path("/issues/%d/labels/%s", n, url.PathEscape(label)), nil, nil)
	if githubapi.StatusOf(err) == 404 {
		return nil
	}
	return err
}

// EnsureLabels creates each label, reconciling one that exists to spec;
// a label that cannot be ensured is reported and the rest still tried.
func (g World) EnsureLabels(labels []workitem.Label) error {
	var failed []error
	for _, l := range labels {
		if err := workitem.RefuseUnapproved(l.Name); err != nil {
			failed = append(failed, err)
			continue
		}
		err := g.c.Call("POST", g.path("/labels"), map[string]any{"name": l.Name, "color": l.Color, "description": l.Description}, nil)
		if githubapi.StatusOf(err) == 422 {
			err = g.c.Call("PATCH", g.path("/labels/%s", url.PathEscape(l.Name)), map[string]any{"color": l.Color, "description": l.Description}, nil)
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("label %q: %w", l.Name, err))
		}
	}
	return errors.Join(failed...)
}

type wireComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	CreatedAt string    `json:"created_at"`
	User      wireLogin `json:"user"`
}

func (g World) Comments(n int) ([]world.Comment, error) {
	var out []world.Comment
	for page := 1; ; page++ {
		var raw []wireComment
		if err := g.c.Call("GET", g.path("/issues/%d/comments?per_page=%d&page=%d", n, world.PageSize, page), nil, &raw); err != nil {
			return nil, gone(err)
		}
		for _, c := range raw {
			out = append(out, world.Comment{ID: c.ID, Body: c.Body, CreatedAt: c.CreatedAt, Author: c.User.Login})
		}
		if len(raw) < world.PageSize {
			return out, nil
		}
	}
}

func (g World) Comment(n int, body string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	err := g.c.Call("POST", g.path("/issues/%d/comments", n), map[string]any{"body": body}, &out)
	return out.ID, gone(err)
}

func (g World) EditComment(id int64, body string) error {
	return gone(g.c.Call("PATCH", g.path("/issues/comments/%d", id), map[string]any{"body": body}, nil))
}

func (g World) Permission(login string) (string, error) {
	var out struct {
		Permission string `json:"permission"`
		RoleName   string `json:"role_name"`
	}
	if err := g.c.Call("GET", g.path("/collaborators/%s/permission", url.PathEscape(login)), nil, &out); err != nil {
		return "", gone(err)
	}
	if out.RoleName != "" {
		return out.RoleName, nil
	}
	if out.Permission != "" {
		return out.Permission, nil
	}
	return "none", nil
}

type wireCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message   string                `json:"message"`
		Author    struct{ Date string } `json:"author"`
		Committer struct{ Date string } `json:"committer"`
	} `json:"commit"`
	Author *wireLogin `json:"author"`
	Files  []struct {
		Filename string `json:"filename"`
	} `json:"files"`
}

func (w wireCommit) login() string {
	if w.Author == nil {
		return ""
	}
	return w.Author.Login
}

func (g World) CommitsPage(branch, since string, page int) ([]world.CommitRef, error) {
	v := url.Values{"sha": {branch}, "per_page": {strconv.Itoa(world.PageSize)}, "page": {strconv.Itoa(page)}}
	if since != "" {
		v.Set("since", since)
	}
	var raw []wireCommit
	if err := g.c.Call("GET", g.path("/commits?%s", v.Encode()), nil, &raw); err != nil {
		return nil, err
	}
	out := make([]world.CommitRef, len(raw))
	for i, c := range raw {
		out[i] = world.CommitRef{SHA: c.SHA, Message: c.Commit.Message, Author: c.login()}
	}
	return out, nil
}

func (g World) Commit(sha string) (world.Commit, error) {
	var c wireCommit
	if err := g.c.Call("GET", g.path("/commits/%s", url.PathEscape(sha)), nil, &c); err != nil {
		return world.Commit{}, gone(err)
	}
	out := world.Commit{SHA: c.SHA, Message: c.Commit.Message, Author: c.login(), Date: c.Commit.Committer.Date, Files: []string{}}
	if out.Date == "" {
		out.Date = c.Commit.Author.Date
	}
	for _, f := range c.Files {
		if f.Filename != "" {
			out.Files = append(out.Files, f.Filename)
		}
	}
	return out, nil
}

type wirePull struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	User      wireLogin `json:"user"`
	UpdatedAt string    `json:"updated_at"`
	MergedAt  string    `json:"merged_at"`
	Head      struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Labels workitem.LabelList `json:"labels"`
	NodeID string             `json:"node_id"`
}

func (w wirePull) pull() world.Pull {
	return world.Pull{Number: w.Number, Title: w.Title, Body: w.Body, State: w.State, Author: w.User.Login,
		HeadRef: w.Head.Ref, HeadSHA: w.Head.SHA, BaseRef: w.Base.Ref, UpdatedAt: w.UpdatedAt, MergedAt: w.MergedAt, Labels: w.Labels, NodeID: w.NodeID}
}

func (g World) PullsPage(state, sortBy, direction string, page int) ([]world.Pull, error) {
	v := url.Values{"state": {state}, "sort": {sortBy}, "direction": {direction}, "per_page": {strconv.Itoa(world.PageSize)}, "page": {strconv.Itoa(page)}}
	var raw []wirePull
	if err := g.c.Call("GET", g.path("/pulls?%s", v.Encode()), nil, &raw); err != nil {
		return nil, err
	}
	out := make([]world.Pull, len(raw))
	for i, p := range raw {
		out[i] = p.pull()
	}
	return out, nil
}

func (g World) PullFilesPage(n, page int) ([]string, error) {
	var raw []struct {
		Filename string `json:"filename"`
	}
	if err := g.c.Call("GET", g.path("/pulls/%d/files?per_page=%d&page=%d", n, world.PageSize, page), nil, &raw); err != nil {
		return nil, err
	}
	out := []string{}
	for _, f := range raw {
		if f.Filename != "" {
			out = append(out, f.Filename)
		}
	}
	return out, nil
}

type wireBranch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

func (g World) BranchesPage(page int) ([]world.Branch, error) {
	var raw []wireBranch
	if err := g.c.Call("GET", g.path("/branches?per_page=%d&page=%d", world.PageSize, page), nil, &raw); err != nil {
		return nil, err
	}
	out := make([]world.Branch, len(raw))
	for i, b := range raw {
		out[i] = world.Branch{Name: b.Name, SHA: b.Commit.SHA}
	}
	return out, nil
}

func (g World) Branch(name string) (world.Branch, error) {
	var b wireBranch
	if err := g.c.Call("GET", g.path("/branches/%s", url.PathEscape(name)), nil, &b); err != nil {
		return world.Branch{}, gone(err)
	}
	return world.Branch{Name: b.Name, SHA: b.Commit.SHA}, nil
}

func (g World) TreePaths(ref string) ([]string, error) {
	var t struct {
		Tree []struct {
			Path string `json:"path"`
		} `json:"tree"`
	}
	if err := g.c.Call("GET", g.path("/git/trees/%s", url.PathEscape(ref)), nil, &t); err != nil {
		return nil, gone(err)
	}
	out := make([]string, 0, len(t.Tree))
	for _, e := range t.Tree {
		out = append(out, e.Path)
	}
	return out, nil
}

func (g World) LatestRelease() (string, error) {
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := g.c.Call("GET", g.path("/releases/latest"), nil, &r); err != nil {
		return "", gone(err)
	}
	return r.TagName, nil
}
