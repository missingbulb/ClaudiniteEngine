package main

// The task queue's half of the stub: the issues API over tasks/sim's
// in-memory repository (issues and pull requests one numbering, comments
// with rising ids and times, labels, close reasons), the repository reads
// the scheduler's signals and the executor's target make over --origin,
// the landing lane's calls, and a routine's fire route:
//
//	POST /routines/{trigger}/fire   records the fire; with --agent, runs
//	                                the stub agent on the item it names
//	POST /_stub/routine   {"down"}: the route answers 503 while down
//	POST /_stub/converge  {"issue","outcome","summary","pr"}: applies the
//	                      transition cn work converge planned, as a routine
//	                      session's GitHub tools would write it

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/items"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/sim"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// routineToken is the routine route's default bearer.
const routineToken = "routine-token"

type fire struct {
	Trigger string `json:"trigger"`
	Text    string `json:"text"`
}

type agentRun struct {
	Issue  int    `json:"issue"`
	Nonce  string `json:"nonce"`
	Exit   int    `json:"exit"`
	Output string `json:"output"`
}

type taskRoutes struct {
	fires       []fire
	agentRuns   []agentRun
	armed       []string
	routineDown bool
}

var (
	firePath        = regexp.MustCompile(`^/routines/([^/]+)/fire$`)
	removeLabelPath = regexp.MustCompile(`^/issues/(\d+)/labels/(.+)$`)
	labelPath       = regexp.MustCompile(`^/labels/(.+)$`)
	commentEditPath = regexp.MustCompile(`^/issues/comments/(\d+)$`)
	permissionPath  = regexp.MustCompile(`^/collaborators/([^/]+)/permission$`)
	pullFilesPath   = regexp.MustCompile(`^/pulls/(\d+)/files$`)
	branchPath      = regexp.MustCompile(`^/branches/(.+)$`)
	treePath        = regexp.MustCompile(`^/git/trees/(.+)$`)
	contentsPath    = regexp.MustCompile(`^/contents/(.+)$`)
	rulesPath       = regexp.MustCompile(`^/rules/branches/(.+)$`)
	refPath         = regexp.MustCompile(`^/git/refs/heads/(.+)$`)
	firePayload     = regexp.MustCompile(`#(\d+)\. Invocation nonce: ([^\s]+)\.$`)
)

// pageOne reports whether the request asks for the first page: every
// list here fits one, so any later page is empty.
func pageOne(r *http.Request) bool {
	p := r.URL.Query().Get("page")
	return p == "" || p == "1"
}

func wireIssue(i sim.StoredIssue) map[string]any {
	labels := []map[string]string{}
	for _, l := range i.Labels {
		labels = append(labels, map[string]string{"name": l})
	}
	author := i.Author
	if author == "" {
		author = bot
	}
	out := map[string]any{"number": i.Number, "title": i.Title, "body": i.Body, "state": i.State, "state_reason": i.StateReason,
		"labels": labels, "user": map[string]string{"login": author}, "created_at": i.CreatedAt, "updated_at": i.UpdatedAt, "closed_at": i.ClosedAt}
	if i.PullRequest {
		var merged any
		if i.MergedAt != "" {
			merged = i.MergedAt
		}
		out["pull_request"] = map[string]any{"merged_at": merged}
	}
	return out
}

func wireComment(c world.Comment) map[string]any {
	return map[string]any{"id": c.ID, "body": c.Body, "created_at": c.CreatedAt, "user": map[string]string{"login": c.Author}}
}

// serveTopLevel answers the routes outside /repos/{r}: the routine fire,
// by its own bearer, and GraphQL's auto-merge arm.
func (s *stub) serveTopLevel(w http.ResponseWriter, r *http.Request, body map[string]any) bool {
	if m := firePath.FindStringSubmatch(r.URL.Path); m != nil && r.Method == http.MethodPost {
		s.fire(w, r, m[1], body)
		return true
	}
	if r.URL.Path != "/graphql" || r.Method != http.MethodPost {
		return false
	}
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		fail(w, http.StatusUnauthorized, "Bad credentials")
		return true
	}
	q, _ := body["query"].(string)
	vars, _ := body["variables"].(map[string]any)
	id, _ := vars["id"].(string)
	if !strings.Contains(q, "enablePullRequestAutoMerge") || id == "" {
		reply(w, 200, map[string]any{"errors": []map[string]string{{"message": "ghstub answers only enablePullRequestAutoMerge"}}})
		return true
	}
	s.tasks.armed = append(s.tasks.armed, id)
	s.calls = append(s.calls, "arm "+id)
	reply(w, 200, map[string]any{"data": map[string]any{"enablePullRequestAutoMerge": map[string]any{"pullRequest": map[string]string{"id": id}}}})
	return true
}

func (s *stub) fire(w http.ResponseWriter, r *http.Request, trigger string, body map[string]any) {
	if r.Header.Get("Authorization") != "Bearer "+s.routineToken {
		reply(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"type": "authentication_error"}})
		return
	}
	if s.tasks.routineDown {
		reply(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"type": "overloaded_error"}})
		return
	}
	text, _ := body["text"].(string)
	s.tasks.fires = append(s.tasks.fires, fire{trigger, text})
	s.calls = append(s.calls, "fire "+trigger)
	session := fmt.Sprintf("session_stub_%d", len(s.tasks.fires))
	reply(w, 200, map[string]string{"claude_code_session_id": session, "claude_code_session_url": "https://claude.ai/code/" + session})
	m := firePayload.FindStringSubmatch(text)
	if s.agent == "" || m == nil {
		return
	}
	n, _ := strconv.Atoi(m[1])
	rec, ok := s.gh.Get(n)
	if !ok {
		return
	}
	base := "https://" + r.Host
	go s.runAgent(rec, m[2], base)
}

// runAgent hands the stub agent the item and its comments as a routine
// session's tools read them, and records how it ended.
func (s *stub) runAgent(rec sim.StoredIssue, nonce, base string) {
	dir, err := os.MkdirTemp("", "ghstub-agent-")
	out := agentRun{Issue: rec.Number, Nonce: nonce, Exit: -1}
	if err == nil {
		defer func() { _ = os.RemoveAll(dir) }()
		item, _ := json.Marshal(wireIssue(rec))
		comments := []map[string]any{}
		for _, c := range rec.Comments {
			comments = append(comments, wireComment(c))
		}
		raw, _ := json.Marshal(comments)
		itemFile, commentsFile := filepath.Join(dir, "item.json"), filepath.Join(dir, "comments.json")
		_ = os.WriteFile(itemFile, item, 0o644)
		_ = os.WriteFile(commentsFile, raw, 0o644)
		cmd := exec.Command(s.agent)
		cmd.Env = append(os.Environ(), "CLAUDINITE_AGENT_ISSUE="+strconv.Itoa(rec.Number), "CLAUDINITE_AGENT_NONCE="+nonce,
			"CLAUDINITE_AGENT_ITEM="+itemFile, "CLAUDINITE_AGENT_COMMENTS="+commentsFile, "CLAUDINITE_AGENT_STUB="+base, "CLAUDINITE_AGENT_REPO="+s.repo)
		got, err := cmd.CombinedOutput()
		out.Output = string(got)
		out.Exit = 0
		if err != nil {
			out.Exit = 1
			if ee, ok := err.(*exec.ExitError); ok {
				out.Exit = ee.ExitCode()
			}
		}
	}
	s.mu.Lock()
	s.tasks.agentRuns = append(s.tasks.agentRuns, out)
	s.mu.Unlock()
}

func (s *stub) taskControl(w http.ResponseWriter, path string, body map[string]any) {
	switch path {
	case "/_stub/routine":
		s.tasks.routineDown, _ = body["down"].(bool)
		reply(w, 200, map[string]any{})
	case "/_stub/converge":
		num := func(k string) int { v, _ := body[k].(float64); return int(v) }
		str := func(k string) string { v, _ := body[k].(string); return v }
		rec, ok := s.gh.Get(num("issue"))
		if !ok {
			fail(w, http.StatusNotFound, "no such issue")
			return
		}
		plan := items.Plan{Issue: rec.Number, Outcome: str("outcome"), Summary: str("summary"), PR: num("pr")}
		if err := items.CheckPlan(plan); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if no := items.Refusal(rec.Issue, rec.Number); no != "" {
			fail(w, http.StatusConflict, no)
			return
		}
		for _, op := range items.ConvergeOps(rec.Issue, plan) {
			s.apply(op)
		}
		s.calls = append(s.calls, fmt.Sprintf("converge %d %s", rec.Number, plan.Outcome))
		reply(w, 200, map[string]any{})
	}
}

// apply performs one converge op as the session's tools would.
func (s *stub) apply(op items.Op) {
	switch op.Kind {
	case "comment":
		_, _ = s.gh.Comment(op.Issue, op.Body)
	case "removeLabel":
		_ = s.gh.RemoveLabel(op.Issue, op.Name)
	case "addLabel":
		_ = s.gh.AddLabel(op.Issue, op.Name)
	case "setBody":
		_ = s.gh.SetIssueBody(op.Issue, op.Body)
	case "close":
		_ = s.gh.CloseIssue(op.Issue, op.StateReason)
	case "closePull":
		_, _ = s.gh.Comment(op.Number, op.Body)
		if p := s.find(op.Number); p != nil {
			p.State = "closed"
		}
		_ = s.gh.CloseIssue(op.Number, "")
	}
}

// serveTasks answers the queue's routes under /repos/{r}.
func (s *stub) serveTasks(w http.ResponseWriter, r *http.Request, path string, body map[string]any) bool {
	q := r.URL.Query()
	str := func(k string) string { v, _ := body[k].(string); return v }
	atoi := func(v string) int { n, _ := strconv.Atoi(v); return n }
	switch {
	case r.Method == http.MethodGet && path == "/issues":
		page := atoi(q.Get("page"))
		if page == 0 {
			page = 1
		}
		state := q.Get("state")
		if state == "" {
			state = "open"
		}
		got, err := s.gh.IssuesPage(world.Query{State: state, Since: q.Get("since"), Label: q.Get("labels"), Sort: q.Get("sort"), Direction: q.Get("direction")}, page)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return true
		}
		out := []map[string]any{}
		for _, i := range got {
			rec, _ := s.gh.Get(i.Number)
			out = append(out, wireIssue(rec))
		}
		reply(w, 200, out)
	case r.Method == http.MethodGet && issuePath.MatchString(path):
		rec, ok := s.gh.Get(atoi(issuePath.FindStringSubmatch(path)[1]))
		if !ok {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		reply(w, 200, wireIssue(rec))
	case r.Method == http.MethodDelete && removeLabelPath.MatchString(path):
		m := removeLabelPath.FindStringSubmatch(path)
		name, _ := url.PathUnescape(m[2])
		rec, ok := s.gh.Get(atoi(m[1]))
		if !ok || !rec.HasLabel(name) {
			fail(w, http.StatusNotFound, "Label does not exist")
			return true
		}
		_ = s.gh.RemoveLabel(rec.Number, name)
		reply(w, 200, []any{})
	case r.Method == http.MethodPost && path == "/labels":
		name := str("name")
		if s.gh.LabelDefined(name) {
			fail(w, http.StatusUnprocessableEntity, "Validation Failed: already_exists")
			return true
		}
		_ = s.gh.EnsureLabels([]workitem.Label{{Name: name, Color: str("color"), Description: str("description")}})
		reply(w, http.StatusCreated, map[string]string{"name": name})
	case r.Method == http.MethodPatch && labelPath.MatchString(path):
		name, _ := url.PathUnescape(labelPath.FindStringSubmatch(path)[1])
		if !s.gh.LabelDefined(name) {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		reply(w, 200, map[string]string{"name": name})
	case r.Method == http.MethodGet && commentsPath.MatchString(path):
		rec, ok := s.gh.Get(atoi(commentsPath.FindStringSubmatch(path)[1]))
		if !ok {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		out := []map[string]any{}
		for _, c := range rec.Comments {
			if pageOne(r) {
				out = append(out, wireComment(c))
			}
		}
		reply(w, 200, out)
	case r.Method == http.MethodPatch && commentEditPath.MatchString(path):
		id, _ := strconv.ParseInt(commentEditPath.FindStringSubmatch(path)[1], 10, 64)
		if err := s.gh.EditComment(id, str("body")); err != nil {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		reply(w, 200, map[string]any{"id": id})
	case r.Method == http.MethodGet && permissionPath.MatchString(path):
		if permissionPath.FindStringSubmatch(path)[1] != s.sess.UserLogin {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		reply(w, 200, map[string]string{"permission": "admin", "role_name": "admin"})
	case r.Method == http.MethodGet && path == "/commits":
		reply(w, 200, s.commits(q.Get("sha"), q.Get("since"), pageOne(r)))
	case r.Method == http.MethodGet && pullFilesPath.MatchString(path):
		p := s.find(atoi(pullFilesPath.FindStringSubmatch(path)[1]))
		if p == nil {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		out := []map[string]string{}
		names, _ := s.git("diff", "--name-only", p.Base+"..."+s.headOf(p.Head))
		for _, n := range strings.Fields(names) {
			if pageOne(r) {
				out = append(out, map[string]string{"filename": n})
			}
		}
		reply(w, 200, out)
	case r.Method == http.MethodGet && path == "/branches":
		out := []map[string]any{}
		refs, _ := s.git("for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads")
		for _, l := range strings.Split(refs, "\n") {
			if f := strings.Fields(l); len(f) == 2 && pageOne(r) {
				out = append(out, map[string]any{"name": f[0], "commit": map[string]string{"sha": f[1]}})
			}
		}
		reply(w, 200, out)
	case r.Method == http.MethodGet && branchPath.MatchString(path):
		name, _ := url.PathUnescape(branchPath.FindStringSubmatch(path)[1])
		sha := s.headOf(name)
		if sha == "" {
			fail(w, http.StatusNotFound, "Branch not found")
			return true
		}
		reply(w, 200, map[string]any{"name": name, "protected": false, "commit": map[string]string{"sha": sha}})
	case r.Method == http.MethodGet && rulesPath.MatchString(path):
		reply(w, 200, []any{})
	case r.Method == http.MethodGet && treePath.MatchString(path):
		ref, _ := url.PathUnescape(treePath.FindStringSubmatch(path)[1])
		names, err := s.git("ls-tree", "--name-only", ref)
		if err != nil {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		tree := []map[string]string{}
		for _, n := range strings.Split(names, "\n") {
			if n != "" {
				tree = append(tree, map[string]string{"path": n})
			}
		}
		reply(w, 200, map[string]any{"tree": tree})
	case r.Method == http.MethodGet && contentsPath.MatchString(path):
		s.contents(w, contentsPath.FindStringSubmatch(path)[1], q.Get("ref"))
	case r.Method == http.MethodGet && path == "/actions/runs":
		var out []run
		for i := len(s.runs) - 1; i >= 0; i-- {
			if s.runs[i].HeadSHA == q.Get("head_sha") {
				out = append(out, s.runs[i])
			}
		}
		reply(w, 200, map[string]any{"total_count": len(out), "workflow_runs": out})
	case r.Method == http.MethodDelete && refPath.MatchString(path):
		ref := refPath.FindStringSubmatch(path)[1]
		if s.headOf(ref) == "" {
			fail(w, http.StatusUnprocessableEntity, "Reference does not exist")
			return true
		}
		_, _ = s.git("update-ref", "-d", "refs/heads/"+ref)
		s.calls = append(s.calls, "delete-branch "+ref)
		reply(w, http.StatusNoContent, nil)
	case r.Method == http.MethodGet && path == "/releases/latest":
		fail(w, http.StatusNotFound, "Not Found")
	default:
		return false
	}
	return true
}

func (s *stub) commitWire(sha, date, message string, files []string) map[string]any {
	fs := []map[string]string{}
	for _, f := range files {
		fs = append(fs, map[string]string{"filename": f})
	}
	return map[string]any{"sha": sha, "commit": map[string]any{"message": message,
		"author": map[string]string{"date": date}, "committer": map[string]string{"date": date}},
		"author": map[string]string{"login": bot}, "files": fs}
}

func (s *stub) commits(branch, since string, first bool) []map[string]any {
	out := []map[string]any{}
	if !first {
		return out
	}
	if branch == "" {
		branch = "main"
	}
	args := []string{"log", "--format=%H%x00%cI%x00%B%x1e", "refs/heads/" + branch}
	if since != "" {
		args = append(args, "--since="+since)
	}
	log, err := s.git(args...)
	if err != nil {
		return out
	}
	for _, rec := range strings.Split(log, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x00", 3)
		if len(f) == 3 {
			out = append(out, s.commitWire(f[0], f[1], strings.TrimSpace(f[2]), nil))
		}
	}
	return out
}

func (s *stub) commit(sha string) (map[string]any, bool) {
	head, err := s.git("show", "-s", "--format=%H%x00%cI%x00%B", sha)
	if err != nil {
		return nil, false
	}
	f := strings.SplitN(head, "\x00", 3)
	if len(f) != 3 {
		return nil, false
	}
	files, _ := s.git("diff-tree", "--no-commit-id", "--name-only", "-r", "--root", sha)
	return s.commitWire(f[0], f[1], strings.TrimSpace(f[2]), strings.Fields(files)), true
}

// contents answers a file's base64 text, or a directory's entries.
func (s *stub) contents(w http.ResponseWriter, path, ref string) {
	if ref == "" {
		ref = "main"
	}
	kind, err := s.git("cat-file", "-t", ref+":"+path)
	switch {
	case err != nil:
		fail(w, http.StatusNotFound, "Not Found")
	case kind == "tree":
		names, _ := s.git("ls-tree", "--name-only", ref+":"+path)
		out := []map[string]string{}
		for _, n := range strings.Split(names, "\n") {
			if n != "" {
				out = append(out, map[string]string{"name": n, "path": path + "/" + n})
			}
		}
		reply(w, 200, out)
	default:
		cmd := exec.Command("git", "--git-dir", s.origin, "show", ref+":"+path)
		raw, err := cmd.Output()
		if err != nil {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		reply(w, 200, map[string]string{"name": filepath.Base(path), "path": path, "content": base64.StdEncoding.EncodeToString(raw), "encoding": "base64"})
	}
}
