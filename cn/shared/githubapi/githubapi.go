// Package githubapi is the few GitHub REST calls the updater makes, with
// the job's own token, which it holds in memory only: the workflow checks
// out without persisting it, cn removes it from its environment on start,
// and of its children only git's remote calls are handed it (gitcmd), so
// the candidate engine an update runs finds it nowhere. Nothing a pack can
// reach calls it. CLAUDINITE_GITHUB_API overrides the base URL for the
// rehearsal's stub, as CLAUDINITE_REGISTRY does for npm.
//
// The calls, each one method here:
//
//	GET   /repos/{repo}/actions/workflows/{workflow}/runs?head_sha=  WorkflowRuns
//	GET   /repos/{repo}/actions/runs?head_sha=                       HeadRuns
//	POST  /repos/{repo}/actions/runs/{id}/approve                    ApproveRun
//	GET   /repos/{repo}/pulls?state=open                             OpenPulls
//	GET   /repos/{repo}/pulls/{n}                                    Pull
//	POST  /repos/{repo}/pulls                                        CreatePull
//	PATCH /repos/{repo}/pulls/{n}  state=closed                      ClosePull
//	PUT   /repos/{repo}/pulls/{n}/merge  squash                      MergePull
//	POST  /repos/{repo}/issues/{n}/comments                          Comment
//	POST  /repos/{repo}/actions/workflows/{workflow}/dispatches      Dispatch
//	GET   /repos/{repo}/issues?state=open&page=                      OpenIssues
//	POST  /repos/{repo}/issues                                       CreateIssue
//	PATCH /repos/{repo}/issues/{n}  body                             UpdateIssueBody
//	PATCH /repos/{repo}/issues/{n}  state=closed                     CloseIssue
package githubapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultBase is GitHub's REST API.
const DefaultBase = "https://api.github.com"

// Client calls one repository's API. Token may be empty: on a Claude Code
// web VM the proxy adds the person's credential.
type Client struct {
	Base  string
	Repo  string // owner/name
	Token string
	HTTP  *http.Client

	calls atomic.Int64
}

// CallCount is how many calls this client has made, reached or not: the
// figure a run's cost record carries.
func (c *Client) CallCount() int64 { return c.calls.Load() }

// Call is one REST call by method and path (the repository's own paths
// begin "/repos/" + Repo), in and out as JSON: the task runner's port is
// written over it, operation by operation, in the caller that names them.
func (c *Client) Call(method, path string, in, out any) error { return c.do(method, path, in, out) }

// HTTPError is an answer outside 2xx; any other error from a call means
// GitHub was not reached.
type HTTPError struct {
	Label      string
	Status     int
	StatusText string
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Label, e.StatusText, e.Message)
}

// StatusOf is the HTTP status an error carries, 0 when GitHub was not
// reached.
func StatusOf(err error) int {
	var h *HTTPError
	if errors.As(err, &h) {
		return h.Status
	}
	return 0
}

// FromEnv builds a client from GITHUB_REPOSITORY, the token given, and
// CLAUDINITE_GITHUB_API.
func FromEnv(token string) (*Client, error) {
	c := &Client{Base: DefaultBase, Repo: os.Getenv("GITHUB_REPOSITORY"), Token: token, HTTP: &http.Client{Timeout: time.Minute}}
	if b := os.Getenv("CLAUDINITE_GITHUB_API"); b != "" {
		c.Base = strings.TrimRight(b, "/")
	}
	if c.Repo == "" || token == "" {
		return nil, errors.New("GITHUB_REPOSITORY and GITHUB_TOKEN must be set (the update workflow's job token)")
	}
	return c, nil
}

func (c *Client) do(method, path string, in, out any) error {
	status, raw, err := c.Raw(method, path, in)
	if err != nil {
		return err
	}
	label := method + " " + strings.SplitN(path, "?", 2)[0]
	if status < 200 || status > 299 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return &HTTPError{Label: label, Status: status, StatusText: fmt.Sprintf("%d %s", status, http.StatusText(status)), Message: msg}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	return nil
}

// Raw is one REST call answered whatever its status: the status and the
// body, an error only when GitHub was not reached. The fleet's client
// judges every status itself, as the Node sweeps did.
func (c *Client) Raw(method, path string, in any) (int, []byte, error) {
	c.calls.Add(1)
	label := method + " " + strings.SplitN(path, "?", 2)[0]
	u, err := url.Parse(c.Base + path)
	if err != nil || u.Scheme != "https" {
		return 0, nil, fmt.Errorf("%s: the GitHub API is called over HTTPS only, not %s", label, c.Base)
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return 0, nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %w", label, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %w", label, err)
	}
	return resp.StatusCode, raw, nil
}

// Run is one workflow run.
type Run struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	CreatedAt  string `json:"created_at"`
}

// WorkflowRuns lists a workflow's runs on one commit, newest first.
func (c *Client) WorkflowRuns(workflow, sha string) ([]Run, error) {
	var out struct {
		Runs []Run `json:"workflow_runs"`
	}
	err := c.do(http.MethodGet, fmt.Sprintf("/repos/%s/actions/workflows/%s/runs?head_sha=%s&per_page=100", c.Repo, url.PathEscape(workflow), url.QueryEscape(sha)), nil, &out)
	return out.Runs, err
}

// HeadRuns lists every workflow's runs on one commit, newest first.
func (c *Client) HeadRuns(sha string) ([]Run, error) {
	var out struct {
		Runs []Run `json:"workflow_runs"`
	}
	err := c.do(http.MethodGet, fmt.Sprintf("/repos/%s/actions/runs?head_sha=%s&per_page=100", c.Repo, url.QueryEscape(sha)), nil, &out)
	return out.Runs, err
}

// ApproveRun approves a run GitHub holds at action_required, as it holds
// the pull_request runs of a pull request the job token opened; the run
// then executes. The token needs actions: write.
func (c *Client) ApproveRun(id int64) error {
	return c.do(http.MethodPost, fmt.Sprintf("/repos/%s/actions/runs/%d/approve", c.Repo, id), nil, nil)
}

// PR is a pull request.
type PR struct {
	Number  int
	Title   string
	Author  string
	Labels  []string
	HeadRef string
	HeadSHA string
	BaseRef string
	State   string
}

// HasLabel reports whether the PR carries label.
func (p PR) HasLabel(label string) bool {
	for _, l := range p.Labels {
		if l == label {
			return true
		}
	}
	return false
}

type wirePR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (w wirePR) pr() PR {
	p := PR{Number: w.Number, Title: w.Title, Author: w.User.Login, HeadRef: w.Head.Ref, HeadSHA: w.Head.SHA, BaseRef: w.Base.Ref, State: w.State}
	for _, l := range w.Labels {
		p.Labels = append(p.Labels, l.Name)
	}
	return p
}

// OpenPulls lists the open pull requests.
func (c *Client) OpenPulls() ([]PR, error) {
	var raw []wirePR
	if err := c.do(http.MethodGet, fmt.Sprintf("/repos/%s/pulls?state=open&per_page=100", c.Repo), nil, &raw); err != nil {
		return nil, err
	}
	out := make([]PR, 0, len(raw))
	for _, w := range raw {
		out = append(out, w.pr())
	}
	return out, nil
}

// Pull reads one pull request.
func (c *Client) Pull(n int) (PR, error) {
	var w wirePR
	err := c.do(http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", c.Repo, n), nil, &w)
	return w.pr(), err
}

// CreatePull opens a pull request of head into base.
func (c *Client) CreatePull(title, body, head, base string) (PR, error) {
	var w wirePR
	err := c.do(http.MethodPost, fmt.Sprintf("/repos/%s/pulls", c.Repo), map[string]string{"title": title, "body": body, "head": head, "base": base}, &w)
	return w.pr(), err
}

// ClosePull closes a pull request unmerged.
func (c *Client) ClosePull(n int) error {
	return c.do(http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%d", c.Repo, n), map[string]string{"state": "closed"}, nil)
}

// MergePull squash-merges a pull request, only while its head is sha.
func (c *Client) MergePull(n int, sha, title string) error {
	return c.do(http.MethodPut, fmt.Sprintf("/repos/%s/pulls/%d/merge", c.Repo, n), map[string]string{"merge_method": "squash", "sha": sha, "commit_title": title}, nil)
}

// Comment comments on an issue or pull request.
func (c *Client) Comment(n int, body string) error {
	return c.do(http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", c.Repo, n), map[string]string{"body": body}, nil)
}

// Dispatch sends workflow_dispatch of workflow on ref with inputs.
func (c *Client) Dispatch(workflow, ref string, inputs map[string]string) error {
	in := map[string]any{"ref": ref}
	if len(inputs) > 0 {
		in["inputs"] = inputs
	}
	return c.do(http.MethodPost, fmt.Sprintf("/repos/%s/actions/workflows/%s/dispatches", c.Repo, url.PathEscape(workflow)), in, nil)
}

// Issue is an open issue.
type Issue struct {
	Number int
	Title  string
	Body   string
}

// OpenIssues lists every open issue (not pull requests).
func (c *Client) OpenIssues() ([]Issue, error) {
	var out []Issue
	for page := 1; ; page++ {
		var raw []struct {
			Number      int             `json:"number"`
			Title       string          `json:"title"`
			Body        string          `json:"body"`
			PullRequest json.RawMessage `json:"pull_request"`
		}
		if err := c.do(http.MethodGet, fmt.Sprintf("/repos/%s/issues?state=open&per_page=100&page=%d", c.Repo, page), nil, &raw); err != nil {
			return nil, err
		}
		for _, r := range raw {
			if r.PullRequest == nil {
				out = append(out, Issue{r.Number, r.Title, r.Body})
			}
		}
		if len(raw) < 100 {
			return out, nil
		}
	}
}

// CreateIssue opens an issue and returns its number.
func (c *Client) CreateIssue(title, body string) (int, error) {
	var out struct {
		Number int `json:"number"`
	}
	err := c.do(http.MethodPost, fmt.Sprintf("/repos/%s/issues", c.Repo), map[string]any{"title": title, "body": body}, &out)
	return out.Number, err
}

// UpdateIssueBody replaces an issue's body.
func (c *Client) UpdateIssueBody(n int, body string) error {
	return c.do(http.MethodPatch, fmt.Sprintf("/repos/%s/issues/%d", c.Repo, n), map[string]string{"body": body}, nil)
}

// CloseIssue closes an issue as not planned.
func (c *Client) CloseIssue(n int) error {
	return c.do(http.MethodPatch, fmt.Sprintf("/repos/%s/issues/%d", c.Repo, n), map[string]string{"state": "closed", "state_reason": "not_planned"}, nil)
}
