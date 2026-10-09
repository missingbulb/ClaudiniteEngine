package ghport

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
)

var (
	_ world.Pulls       = World{}
	_ land.API          = World{}
	_ execute.SDKGitHub = World{}
)

// statusErr carries an API answer's status to the lane, which reads it.
func statusErr(err error) error {
	var h *githubapi.HTTPError
	if errors.As(err, &h) {
		return &land.StatusError{Status: h.Status, Message: h.Error()}
	}
	return err
}

// refPath escapes a ref segment by segment, keeping its slashes.
func refPath(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (g World) Pull(n int) (world.Pull, error) {
	var w wirePull
	if err := g.c.Call("GET", g.path("/pulls/%d", n), nil, &w); err != nil {
		return world.Pull{}, gone(err)
	}
	return w.pull(), nil
}

func (g World) Mergeable(n int) (*bool, error) {
	var w struct {
		Mergeable *bool `json:"mergeable"`
	}
	if err := g.c.Call("GET", g.path("/pulls/%d", n), nil, &w); err != nil {
		return nil, gone(err)
	}
	return w.Mergeable, nil
}

func (g World) ClosePull(n int) error {
	return g.c.Call("PATCH", g.path("/pulls/%d", n), map[string]string{"state": "closed"}, nil)
}

func (g World) CreatePull(title, body, head, base string) (world.Pull, error) {
	var w wirePull
	err := g.c.Call("POST", g.path("/pulls"), map[string]string{"title": title, "body": body, "head": head, "base": base}, &w)
	return w.pull(), err
}

type wireContent struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func decodeContent(c wireContent) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(c.Content, "\n", ""))
	return string(raw), err
}

func (g World) FileAt(path, ref string) (string, error) {
	var c wireContent
	if err := g.c.Call("GET", g.path("/contents/%s?ref=%s", refPath(path), url.QueryEscape(ref)), nil, &c); err != nil {
		return "", gone(err)
	}
	return decodeContent(c)
}

func (g World) WorkflowFiles(ref string) ([]land.WorkflowFile, error) {
	var dir []wireContent
	if err := g.c.Call("GET", g.path("/contents/.github/workflows?ref=%s", url.QueryEscape(ref)), nil, &dir); err != nil {
		if githubapi.StatusOf(err) == 404 {
			return nil, nil
		}
		return nil, err
	}
	var out []land.WorkflowFile
	for _, e := range dir {
		if !strings.HasSuffix(e.Name, ".yml") && !strings.HasSuffix(e.Name, ".yaml") {
			continue
		}
		text, err := g.FileAt(".github/workflows/"+e.Name, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, land.WorkflowFile{Name: e.Name, Content: text})
	}
	return out, nil
}

func (g World) DispatchWorkflow(name, ref string) error {
	return statusErr(g.c.Call("POST", g.path("/actions/workflows/%s/dispatches", url.PathEscape(name)), map[string]string{"ref": ref}, nil))
}

func (g World) BranchProtected(base string) (*bool, error) {
	var b struct {
		Protected *bool `json:"protected"`
	}
	if err := g.c.Call("GET", g.path("/branches/%s", refPath(base)), nil, &b); err != nil {
		return nil, err
	}
	if b.Protected == nil {
		return nil, fmt.Errorf("the branch %s answered no protected flag", base)
	}
	return b.Protected, nil
}

func (g World) BranchRules(base string) ([]string, error) {
	var rules []struct {
		Type string `json:"type"`
	}
	if err := g.c.Call("GET", g.path("/rules/branches/%s", refPath(base)), nil, &rules); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Type)
	}
	return out, nil
}

func (g World) RunsForSHA(sha string) ([]land.Run, error) {
	var w struct {
		Runs []land.Run `json:"workflow_runs"`
	}
	if err := g.c.Call("GET", g.path("/actions/runs?head_sha=%s&per_page=100", url.QueryEscape(sha)), nil, &w); err != nil {
		return nil, err
	}
	return w.Runs, nil
}

// ApproveRun approves a run GitHub holds at action_required, which then
// executes; the job token needs actions: write.
func (g World) ApproveRun(id int64) error {
	return statusErr(g.c.Call("POST", g.path("/actions/runs/%d/approve", id), nil, nil))
}

// EnableAutoMerge arms native auto-merge, which waits for the checks;
// the REST merge would merge now, past them.
func (g World) EnableAutoMerge(nodeID string) error {
	var answer struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	q := map[string]any{
		"query":     "mutation($id:ID!){enablePullRequestAutoMerge(input:{pullRequestId:$id,mergeMethod:SQUASH}){pullRequest{id}}}",
		"variables": map[string]string{"id": nodeID},
	}
	if err := g.c.Call("POST", "/graphql", q, &answer); err != nil {
		return statusErr(err)
	}
	if len(answer.Errors) > 0 {
		return errors.New(answer.Errors[0].Message)
	}
	return nil
}

func (g World) MergePull(m land.Merge) error {
	in := map[string]string{"merge_method": "squash"}
	if m.SHA != "" {
		in["sha"] = m.SHA
	}
	if m.Title != "" {
		in["commit_title"] = m.Title
	}
	if m.Message != "" {
		in["commit_message"] = m.Message
	}
	return statusErr(g.c.Call("PUT", g.path("/pulls/%d/merge", m.Number), in, nil))
}

func (g World) DeleteBranch(ref string) error {
	return g.c.Call("DELETE", g.path("/git/refs/heads/%s", refPath(ref)), nil, nil)
}
