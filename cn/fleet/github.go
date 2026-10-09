package fleet

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
)

// Response is one call's status and body, null for none.
type Response struct {
	Status int
	JSON   json.RawMessage
}

// GH is one REST call by method and path, answered whatever its status;
// an error means GitHub was not reached. It is the Node sweeps' gh shape,
// so a test fakes the whole API with a table.
type GH func(method, path string, body any) (Response, error)

// NewGH is a GH over the GitHub REST API at base with token.
func NewGH(base, token string) GH {
	c := &githubapi.Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: time.Minute}}
	return func(method, path string, body any) (Response, error) {
		status, raw, err := c.Raw(method, path, body)
		if err != nil {
			return Response{}, err
		}
		if len(raw) == 0 {
			raw = nil
		}
		return Response{Status: status, JSON: raw}, nil
	}
}

// Get is a GET.
func (gh GH) Get(path string) (Response, error) { return gh("GET", path, nil) }

// GrantError is a refusal the token's grant explains: a person adds a
// scope. The sweep prints the action marker for it, which the executor's
// park comment names as the worker's own verdict.
type GrantError struct{ Msg string }

func (e *GrantError) Error() string { return e.Msg }

// Triage is the marker's kind for a grant error.
const Triage = "action"

// IsGrant reports whether err is a grant error.
func IsGrant(err error) bool {
	var g *GrantError
	return errors.As(err, &g)
}

// Paged reads every page of a listing. A 401 or 403 is a grant error
// naming the permission the path most likely lacks.
func Paged(gh GH, path string) ([]json.RawMessage, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var all []json.RawMessage
	for page := 1; ; page++ {
		r, err := gh.Get(fmt.Sprintf("%s%sper_page=100&page=%d", path, sep, page))
		if err != nil {
			return nil, err
		}
		var items []json.RawMessage
		if r.Status != 200 || json.Unmarshal(r.JSON, &items) != nil || items == nil {
			why := fmt.Sprintf("GET %s page %d failed with status %d", path, page, r.Status)
			switch r.Status {
			case 401:
				return nil, &GrantError{why + " — the fleet PAT is unusable"}
			case 403:
				return nil, &GrantError{why + ForbiddenHint(path)}
			}
			return nil, errors.New(why)
		}
		all = append(all, items...)
		if len(items) < 100 {
			return all, nil
		}
	}
}

// Expect is one write that must answer want: a 403 is a grant error
// carrying the hint for path, any other status an error naming it.
func Expect(gh GH, method, path string, body any, want int) (Response, error) {
	r, err := gh(method, path, body)
	if err != nil {
		return r, err
	}
	if r.Status == want {
		return r, nil
	}
	msg := fmt.Sprintf("%s %s returned %d", method, path, r.Status)
	if r.Status == 403 {
		return r, &GrantError{msg + ForbiddenHint(path)}
	}
	return r, errors.New(msg)
}

// EnsureLabel creates a label in repo; 422 is "already exists".
func EnsureLabel(gh GH, repo, name, color, description string) error {
	r, err := gh("POST", "/repos/"+repo+"/labels", map[string]string{"name": name, "color": color, "description": description})
	if err != nil {
		return err
	}
	if r.Status != 201 && r.Status != 422 {
		return fmt.Errorf("creating label %s returned %d", name, r.Status)
	}
	return nil
}

// Issue is an issue as a labelled listing returns it.
type Issue struct {
	Number      int                `json:"number"`
	Title       string             `json:"title"`
	Body        string             `json:"body"`
	Labels      workitem.LabelList `json:"labels"`
	State       string             `json:"state"`
	StateReason *string            `json:"state_reason"`
	ClosedAt    *string            `json:"closed_at"`
	PullRequest json.RawMessage    `json:"pull_request"`
}

// TitledIssues is every issue in repo whose title titled accepts, open
// and closed: the title is the key a sweep converges its issues by.
func TitledIssues(gh GH, repo string, titled func(string) bool) (open, closed []Issue, err error) {
	raw, err := Paged(gh, "/repos/"+repo+"/issues?state=all")
	if err != nil {
		return nil, nil, err
	}
	for _, r := range raw {
		var i Issue
		if json.Unmarshal(r, &i) != nil || (len(i.PullRequest) > 0 && string(i.PullRequest) != "null") || !titled(i.Title) {
			continue
		}
		switch i.State {
		case "open":
			open = append(open, i)
		case "closed":
			closed = append(closed, i)
		}
	}
	return open, closed, nil
}

// contentsPath is a repository file's contents endpoint, the path escaped
// as encodeURI escapes it.
func contentsPath(repo, path string) string {
	return "/repos/" + repo + "/contents/" + (&url.URL{Path: path}).EscapedPath()
}

// File is one file's text and blob sha.
type File struct {
	Text, SHA string
}

// ReadFile is one file of repo's default branch, nil when it has none.
func ReadFile(gh GH, repo, path string) (*File, error) {
	r, err := gh.Get(contentsPath(repo, path))
	if err != nil {
		return nil, err
	}
	if r.Status == 404 {
		return nil, nil
	}
	var body struct {
		Content *string `json:"content"`
		SHA     string  `json:"sha"`
	}
	if r.Status != 200 || json.Unmarshal(r.JSON, &body) != nil || body.Content == nil {
		msg := fmt.Sprintf("%s:%s returned %d", repo, path, r.Status)
		if r.Status == 403 {
			return nil, &GrantError{msg + ForbiddenHint("/repos/"+repo+"/contents/")}
		}
		return nil, errors.New(msg)
	}
	text, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(*body.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("%s:%s is not base64: %v", repo, path, err)
	}
	return &File{Text: string(text), SHA: body.SHA}, nil
}

// FileExists is 200 → true, 404 → false, anything else an error.
func FileExists(gh GH, repo, path string) (bool, error) {
	r, err := gh.Get(contentsPath(repo, path))
	if err != nil {
		return false, err
	}
	switch r.Status {
	case 200:
		return true, nil
	case 404:
		return false, nil
	case 403:
		return false, &GrantError{fmt.Sprintf("%s:%s returned 403%s", repo, path, ForbiddenHint("/repos/"+repo+"/contents/"))}
	}
	return false, fmt.Errorf("%s:%s returned %d", repo, path, r.Status)
}

// DirEntry is one entry of a directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ListDir is a directory of repo's default branch, nil when absent.
func ListDir(gh GH, repo, path string) ([]DirEntry, bool, error) {
	r, err := gh.Get(contentsPath(repo, path))
	if err != nil {
		return nil, false, err
	}
	if r.Status == 404 {
		return nil, false, nil
	}
	var entries []DirEntry
	if r.Status != 200 || json.Unmarshal(r.JSON, &entries) != nil {
		msg := fmt.Sprintf("%s:%s returned %d", repo, path, r.Status)
		if r.Status == 403 {
			return nil, false, &GrantError{msg + ForbiddenHint("/repos/"+repo+"/contents/")}
		}
		if r.Status == 200 {
			msg = fmt.Sprintf("%s:%s is not a directory", repo, path)
		}
		return nil, false, errors.New(msg)
	}
	return entries, true, nil
}

// PutFile writes one file to repo's default branch, guarded by the blob
// sha its read returned (empty for a new file): the one function under
// fleet/ that writes into a member's tree. A 409 is the file moving under
// the sweep, so this run does not write it and the next re-reads; a 403
// or 404 is the token's grant.
func PutFile(gh GH, repo, path, text, sha, message string) error {
	body := map[string]string{"message": message, "content": base64.StdEncoding.EncodeToString([]byte(text))}
	if sha != "" {
		body["sha"] = sha
	}
	r, err := gh("PUT", contentsPath(repo, path), body)
	if err != nil {
		return err
	}
	var msg struct {
		Message *string `json:"message"`
	}
	_ = json.Unmarshal(r.JSON, &msg)
	why := "no message"
	if msg.Message != nil {
		why = *msg.Message
	}
	switch r.Status {
	case 200, 201:
		return nil
	case 409:
		return fmt.Errorf("%s:%s changed under the sweep (409) — not written this run", repo, path)
	case 403, 404:
		return &GrantError{fmt.Sprintf("writing %s:%s returned %d%s (%s)", repo, path, r.Status, ForbiddenHint("/repos/"+repo+"/contents/"), why)}
	}
	return fmt.Errorf("writing %s:%s returned %d (%s)", repo, path, r.Status, why)
}

// EncodeURIComponent is JavaScript's encodeURIComponent: every byte but
// A-Z a-z 0-9 - _ . ! ~ * ' ( ) percent-encoded, as a label's name goes
// into a path segment.
func EncodeURIComponent(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', strings.IndexByte("-_.!~*'()", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// Scheduler is the member-side workflow every fan-out fires.
const Scheduler = "claudinite-scheduler.yml"

// SchedulerPath is where a member carries it.
const SchedulerPath = ".github/workflows/" + Scheduler

// Dispatch is a dispatch's verdict: fired, or why nothing was queued.
type Dispatch struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// FireScheduler dispatches one member's scheduler on its default branch,
// waking task (the empty string wakes nothing).
func FireScheduler(gh GH, repo, ref, task string) (Dispatch, error) {
	r, err := gh("POST", "/repos/"+repo+"/actions/workflows/"+Scheduler+"/dispatches",
		map[string]any{"ref": ref, "inputs": map[string]string{"wake": task}})
	if err != nil {
		return Dispatch{}, err
	}
	return ClassifyDispatch(r.Status), nil
}

// ClassifyDispatch is what a dispatch POST's status means; each state is
// a different thing for the reader to do.
func ClassifyDispatch(status int) Dispatch {
	switch status {
	case 204:
		return Dispatch{"fired", "queued on its own scheduler"}
	case 404:
		return Dispatch{"no-scheduler", "no " + Scheduler + " on the default branch, or Actions is disabled — nothing there can run its own tasks"}
	case 403:
		return Dispatch{"no-permission", "dispatching a workflow was refused" + ForbiddenHint("/actions/workflows/dispatches")}
	case 422:
		return Dispatch{"not-dispatchable", "the workflow exists but refused the dispatch (422) — either its scheduler workflow predates the `wake` input, which its next update lands, or GitHub has disabled it (cron is switched off on inactive repos)"}
	}
	return Dispatch{"error", fmt.Sprintf("dispatch returned %d", status)}
}

func jsonUnmarshal(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }

type statusError struct {
	what   string
	status int
}

func (e *statusError) Error() string { return fmt.Sprintf("%s returned %d", e.what, e.status) }
