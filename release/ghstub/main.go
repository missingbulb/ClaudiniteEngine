// Command ghstub stands in for the GitHub REST calls the updater makes
// (shared/githubapi), over HTTPS on loopback, for the rehearsal's update
// mode and the release workflow's hop job. Pull requests read their head
// from --origin, a bare repository, and a merge squashes onto its main
// there, as GitHub would. cn reaches it through CLAUDINITE_GITHUB_API.
//
// For a session's key request it answers GET /user, GET /repos/{r} and
// its commits and check runs, and a claudinite-key repository_dispatch,
// which it forwards to licstub (--licstub-ready, --licstub-ca) and keeps
// licstub's answer as the key check run; the device flow pair; and an
// Actions job's OIDC token at GET /_oidc/token (session.go). For the
// task queue it answers the issues, repository and landing-lane calls
// over an in-memory repository, and a routine's fire route (tasks.go).
//
// Control endpoints, unauthenticated, for the script driving it:
//
//	POST /_stub/session   replaces the session fields it names (session.go)
//	POST /_stub/run       {"sha"|"ref", "event", "status", "conclusion"}
//	                      adds a claudinite-ci.yml run (event push,
//	                      status completed by default)
//	POST /_stub/dispatch  {"conclusion"}: every later workflow_dispatch
//	                      adds a run with it on the ref's head; "" adds none
//	GET  /_stub/state     pulls, issues, dispatches, fires, agent runs,
//	                      armed auto-merges and the call log
//
// For a fleet manager's sweeps it answers GET /user/repos and every call
// on a fleet member, each a directory on disk (fleet.go).
//
// It writes its base URL to --ready once listening and the certificate to
// --ca-out.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/release/stubtls"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/sim"
)

const bot = "github-actions[bot]"

type pull struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Head   string   `json:"head"`
	Base   string   `json:"base"`
	State  string   `json:"state"`
	Merged bool     `json:"merged"`
	Labels []string `json:"labels"`
	// HeadSHA is the head a merge squashed, kept once the branch is gone.
	HeadSHA string `json:"head_sha,omitempty"`
}

// issue is an issue as the state answers it.
type issue struct {
	Number      int      `json:"number"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Labels      []string `json:"labels"`
	State       string   `json:"state"`
	StateReason string   `json:"state_reason"`
	Comments    []string `json:"comments"`
}

type run struct {
	ID         int64  `json:"id"`
	HeadSHA    string `json:"head_sha"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	CreatedAt  string `json:"created_at"`
	HTMLURL    string `json:"html_url"`
}

type dispatch struct {
	Workflow string            `json:"workflow"`
	Ref      string            `json:"ref"`
	Inputs   map[string]string `json:"inputs"`
	// Repo is a fleet member's; the manager's own dispatches leave it out.
	Repo string `json:"repo,omitempty"`
}

// stubState is what GET /_stub/state answers.
type stubState struct {
	Pulls      []pull     `json:"pulls"`
	Issues     []issue    `json:"issues"`
	Dispatches []dispatch `json:"dispatches"`
	Calls      []string   `json:"calls"`
	Fires      []fire     `json:"fires"`
	Agent      []agentRun `json:"agent"`
	Armed      []string   `json:"armed"`
	// Fleet is each member's issues, by owner/name.
	Fleet map[string][]issue `json:"fleet"`
}

type stub struct {
	mu     sync.Mutex
	origin string
	repo   string
	token  string
	runID  int64
	clock  time.Time
	pulls  []*pull
	runs   []run
	onDisp string
	disps  []dispatch
	calls  []string
	// gh holds the issues, pull requests' issue records among them, so the
	// two share one numbering as GitHub's do.
	gh       *sim.GitHub
	simClock *sim.Clock
	tasks    taskRoutes
	// agent is the stub agent a routine fire runs; "" runs none.
	agent string
	// routineToken is the bearer the routine route accepts.
	routineToken string

	sess        session
	checkRuns   map[string][]checkRun
	devicePolls int
	rsaKey      *rsa.PrivateKey
	licReady    string
	licCA       string

	// fleet is the members a fleet token reaches beside the repo (fleet.go).
	fleet []*fleetMember
}

// defaultSession is a public repo of a User, person 7 with push access,
// the App installed.
var defaultSession = session{UserID: 7, UserLogin: "acme-dev", UserType: "User", RepoID: 1001, OwnerID: 3, OwnerType: "User"}

func newStub(origin, repo, token string) *stub {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	clock := sim.NewClock(time.Now())
	gh := sim.NewGitHub(clock)
	gh.Repo = repo
	return &stub{origin: origin, repo: repo, token: token, clock: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		gh: gh, simClock: clock, routineToken: routineToken, sess: defaultSession, checkRuns: map[string][]checkRun{}, rsaKey: k}
}

func (s *stub) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--git-dir", s.origin}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME="+bot, "GIT_AUTHOR_EMAIL=41898282+github-actions[bot]@users.noreply.github.com",
		"GIT_COMMITTER_NAME=GitHub", "GIT_COMMITTER_EMAIL=noreply@github.com")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (s *stub) headOf(ref string) string {
	sha, err := s.git("rev-parse", "--verify", "--quiet", "refs/heads/"+ref)
	if err != nil {
		return ""
	}
	return sha
}

func (s *stub) addRun(sha, event, status, conclusion string) {
	s.runID++
	s.clock = s.clock.Add(time.Minute)
	if status == "" {
		status = "completed"
	}
	if event == "" {
		event = "push"
	}
	s.runs = append(s.runs, run{ID: s.runID, HeadSHA: sha, Event: event, Status: status, Conclusion: conclusion, CreatedAt: s.clock.Format(time.RFC3339),
		HTMLURL: fmt.Sprintf("https://github.com/%s/actions/runs/%d", s.repo, s.runID)})
}

func (s *stub) wire(p *pull) map[string]any {
	labels := []map[string]string{}
	rec, _ := s.gh.Get(p.Number)
	p.Labels = append([]string{}, rec.Labels...)
	for _, l := range p.Labels {
		labels = append(labels, map[string]string{"name": l})
	}
	var merged any
	if rec.MergedAt != "" {
		merged = rec.MergedAt
	}
	return map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "merged": p.Merged,
		"node_id": fmt.Sprintf("PR_%d", p.Number), "mergeable": p.State == "open", "merged_at": merged, "updated_at": rec.UpdatedAt,
		"user": map[string]string{"login": bot}, "labels": labels,
		"head": map[string]string{"ref": p.Head, "sha": s.headOf(p.Head)}, "base": map[string]string{"ref": p.Base}}
}

func (s *stub) find(n int) *pull {
	for _, p := range s.pulls {
		if p.Number == n {
			return p
		}
	}
	return nil
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"message": msg})
}

var (
	runsPath     = regexp.MustCompile(`^/actions/workflows/([^/]+)/runs$`)
	dispatchPath = regexp.MustCompile(`^/actions/workflows/([^/]+)/dispatches$`)
	runPath      = regexp.MustCompile(`^/actions/runs/(\d+)$`)
	pullPath     = regexp.MustCompile(`^/pulls/(\d+)$`)
	mergePath    = regexp.MustCompile(`^/pulls/(\d+)/merge$`)
	labelsPath   = regexp.MustCompile(`^/issues/(\d+)/labels$`)
	commentsPath = regexp.MustCompile(`^/issues/(\d+)/comments$`)
	issuePath    = regexp.MustCompile(`^/issues/(\d+)$`)
)

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body map[string]any
	if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut) {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	str := func(k string) string { v, _ := body[k].(string); return v }

	s.simClock.Set(time.Now())
	if strings.HasPrefix(r.URL.Path, "/_stub/") {
		s.control(w, r, str, body)
		return
	}
	if s.serveTopLevel(w, r, body) {
		return
	}
	if s.serveFleet(w, r, body) {
		return
	}
	if s.serveSession(w, r, body) {
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		fail(w, http.StatusUnauthorized, "Bad credentials")
		return
	}
	prefix := "/repos/" + s.repo
	if !strings.HasPrefix(r.URL.Path, prefix+"/") {
		fail(w, http.StatusNotFound, "Not Found")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, prefix)
	num := func(re *regexp.Regexp) int {
		m := re.FindStringSubmatch(path)
		if m == nil {
			return 0
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}

	switch {
	case r.Method == http.MethodGet && runsPath.MatchString(path):
		// Keyed on head_sha, as the update's CI follow asks; a worker following its
		// own dispatch has no sha and asks by event instead, newest first either way.
		q := r.URL.Query()
		sha, event := q.Get("head_sha"), q.Get("event")
		var out []run
		for i := len(s.runs) - 1; i >= 0; i-- {
			if sha != "" || event == "" {
				if s.runs[i].HeadSHA == sha {
					out = append(out, s.runs[i])
				}
			} else if s.runs[i].Event == event {
				out = append(out, s.runs[i])
			}
		}
		reply(w, 200, map[string]any{"total_count": len(out), "workflow_runs": out})
	case r.Method == http.MethodGet && runPath.MatchString(path):
		id, _ := strconv.ParseInt(runPath.FindStringSubmatch(path)[1], 10, 64)
		for _, run := range s.runs {
			if run.ID == id {
				reply(w, 200, run)
				return
			}
		}
		fail(w, http.StatusNotFound, "Not Found")
	case r.Method == http.MethodGet && path == "/pages":
		reply(w, 200, map[string]string{"build_type": "workflow"})
	case r.Method == http.MethodPost && dispatchPath.MatchString(path):
		wf := dispatchPath.FindStringSubmatch(path)[1]
		inputs := map[string]string{}
		if in, ok := body["inputs"].(map[string]any); ok {
			for k, v := range in {
				inputs[k], _ = v.(string)
			}
		}
		head := s.headOf(str("ref"))
		if head == "" {
			fail(w, http.StatusUnprocessableEntity, "No ref found for: "+str("ref"))
			return
		}
		s.disps = append(s.disps, dispatch{Workflow: wf, Ref: str("ref"), Inputs: inputs})
		s.calls = append(s.calls, fmt.Sprintf("dispatch %s %s pr=%s", wf, str("ref"), inputs["pr"]))
		if s.onDisp != "" {
			s.addRun(head, "workflow_dispatch", "completed", s.onDisp)
		}
		reply(w, http.StatusNoContent, nil)
	case r.Method == http.MethodGet && path == "/pulls":
		out := []map[string]any{}
		state := r.URL.Query().Get("state")
		for i := len(s.pulls) - 1; i >= 0 && pageOne(r); i-- {
			if p := s.pulls[i]; state == "all" || p.State == "open" && state != "closed" || p.State == "closed" && state == "closed" {
				out = append(out, s.wire(p))
			}
		}
		reply(w, 200, out)
	case r.Method == http.MethodPost && path == "/pulls":
		if s.headOf(str("head")) == "" || s.headOf(str("base")) == "" {
			fail(w, http.StatusUnprocessableEntity, "Validation Failed: head or base does not exist")
			return
		}
		n := s.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: str("title"), Body: str("body")}, PullRequest: true, Author: bot})
		p := &pull{Number: n, Title: str("title"), Body: str("body"), Head: str("head"), Base: str("base"), State: "open", Labels: []string{}}
		s.pulls = append(s.pulls, p)
		s.calls = append(s.calls, fmt.Sprintf("create-pull %d %s", p.Number, p.Head))
		reply(w, http.StatusCreated, s.wire(p))
	case pullPath.MatchString(path) && (r.Method == http.MethodGet || r.Method == http.MethodPatch):
		p := s.find(num(pullPath))
		if p == nil {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		if r.Method == http.MethodPatch && str("state") == "closed" {
			p.State = "closed"
			_ = s.gh.CloseIssue(p.Number, "")
			s.calls = append(s.calls, fmt.Sprintf("close-pull %d", p.Number))
		}
		reply(w, 200, s.wire(p))
	case r.Method == http.MethodPut && mergePath.MatchString(path):
		s.merge(w, s.find(num(mergePath)), str("sha"), str("commit_title"), str("commit_message"), str("merge_method"))
	default:
		if s.serveIssues(w, r, path, body) || s.serveTasks(w, r, path, body) {
			return
		}
		fail(w, http.StatusNotFound, "ghstub does not answer "+r.Method+" "+path)
	}
}

// serveIssues answers the issue writes on s.gh, the manager's store or,
// swapped in by serveFleet, a member's.
func (s *stub) serveIssues(w http.ResponseWriter, r *http.Request, path string, body map[string]any) bool {
	str := func(k string) string { v, _ := body[k].(string); return v }
	num := func(re *regexp.Regexp) int {
		n, _ := strconv.Atoi(re.FindStringSubmatch(path)[1])
		return n
	}
	switch {
	case r.Method == http.MethodPost && labelsPath.MatchString(path):
		n := num(labelsPath)
		var labels []string
		if ls, ok := body["labels"].([]any); ok {
			for _, l := range ls {
				if v, ok := l.(string); ok {
					labels = append(labels, v)
				}
			}
		}
		for _, l := range labels {
			if err := s.gh.AddLabel(n, l); err != nil {
				fail(w, http.StatusNotFound, "Not Found")
				return true
			}
		}
		s.calls = append(s.calls, fmt.Sprintf("label %d %s", n, strings.Join(labels, ",")))
		reply(w, 200, []any{})
	case r.Method == http.MethodPost && commentsPath.MatchString(path):
		n := num(commentsPath)
		id, err := s.gh.Comment(n, str("body"))
		if err != nil {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		s.calls = append(s.calls, fmt.Sprintf("comment %d", n))
		reply(w, http.StatusCreated, map[string]any{"id": id})
	case r.Method == http.MethodPost && path == "/issues":
		var labels []string
		if ls, ok := body["labels"].([]any); ok {
			for _, l := range ls {
				if v, ok := l.(string); ok {
					labels = append(labels, v)
				}
			}
		}
		n, _ := s.gh.CreateIssue(str("title"), str("body"), labels)
		s.calls = append(s.calls, fmt.Sprintf("create-issue %d %s", n, str("title")))
		reply(w, http.StatusCreated, map[string]any{"number": n})
	case r.Method == http.MethodPatch && issuePath.MatchString(path):
		n := num(issuePath)
		if _, ok := s.gh.Get(n); !ok {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		if v, ok := body["body"].(string); ok {
			_ = s.gh.SetIssueBody(n, v)
		}
		if v, ok := body["title"].(string); ok {
			_ = s.gh.SetIssueTitle(n, v)
		}
		switch str("state") {
		case "closed":
			_ = s.gh.CloseIssue(n, str("state_reason"))
		case "open":
			_ = s.gh.ReopenIssue(n)
		}
		s.calls = append(s.calls, fmt.Sprintf("update-issue %d", n))
		rec, _ := s.gh.Get(n)
		reply(w, 200, wireIssue(rec))
	default:
		return false
	}
	return true
}

func wireIssues(gh *sim.GitHub) []issue {
	out := []issue{}
	for _, is := range gh.All() {
		if is.PullRequest {
			continue
		}
		v := issue{Number: is.Number, Title: is.Title, Body: is.Body, Labels: append([]string{}, is.Labels...), State: is.State, StateReason: is.StateReason, Comments: []string{}}
		for _, c := range is.Comments {
			v.Comments = append(v.Comments, c.Body)
		}
		out = append(out, v)
	}
	return out
}

// merge squashes the PR's head onto its base in origin, refusing a closed
// PR or a head that moved from sha, as GitHub does.
func (s *stub) merge(w http.ResponseWriter, p *pull, sha, title, message, method string) {
	switch {
	case p == nil:
		fail(w, http.StatusNotFound, "Not Found")
		return
	case p.State != "open":
		fail(w, http.StatusMethodNotAllowed, "Pull Request is not mergeable")
		return
	case method != "squash":
		fail(w, http.StatusMethodNotAllowed, "ghstub merges by squash only")
		return
	case s.headOf(p.Head) != sha:
		fail(w, http.StatusConflict, "Head branch was modified. Review and try the merge again.")
		return
	}
	base := s.headOf(p.Base)
	p.HeadSHA = sha
	if title == "" {
		title = fmt.Sprintf("%s (#%d)", p.Title, p.Number)
	}
	msg := title
	if message != "" {
		msg += "\n\n" + message
	}
	tree, err := s.git("rev-parse", sha+"^{tree}")
	if err == nil {
		var commit string
		commit, err = s.git("commit-tree", tree, "-p", base, "-m", msg)
		if err == nil {
			_, err = s.git("update-ref", "refs/heads/"+p.Base, commit, base)
		}
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "squash: "+err.Error())
		return
	}
	p.State, p.Merged = "closed", true
	_ = s.gh.MarkMerged(p.Number)
	s.calls = append(s.calls, fmt.Sprintf("merge %d %s", p.Number, sha))
	reply(w, 200, map[string]any{"merged": true})
}

func (s *stub) control(w http.ResponseWriter, r *http.Request, str func(string) string, body map[string]any) {
	switch r.URL.Path {
	case "/_stub/session":
		raw, _ := json.Marshal(body)
		if err := json.Unmarshal(raw, &s.sess); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		reply(w, 200, s.sess)
	case "/_stub/run":
		sha := str("sha")
		if sha == "" {
			sha = s.headOf(str("ref"))
		}
		if sha == "" {
			fail(w, http.StatusBadRequest, "no sha or ref")
			return
		}
		s.addRun(sha, str("event"), str("status"), str("conclusion"))
		reply(w, 200, map[string]string{"sha": sha})
	case "/_stub/dispatch":
		s.onDisp = str("conclusion")
		reply(w, 200, map[string]string{})
	case "/_stub/state":
		st := stubState{Pulls: []pull{}, Issues: []issue{}, Dispatches: append([]dispatch{}, s.disps...), Calls: append([]string{}, s.calls...),
			Fires: append([]fire{}, s.tasks.fires...), Agent: append([]agentRun{}, s.tasks.agentRuns...), Armed: append([]string{}, s.tasks.armed...)}
		for _, p := range s.pulls {
			s.wire(p)
			st.Pulls = append(st.Pulls, *p)
		}
		st.Issues = wireIssues(s.gh)
		st.Fleet = map[string][]issue{}
		for _, m := range s.fleet {
			st.Fleet[m.full] = wireIssues(m.gh)
		}
		reply(w, 200, st)
	case "/_stub/routine", "/_stub/converge":
		s.taskControl(w, r.URL.Path, body)
	case "/_stub/advance", "/_stub/deny":
		s.fleetControl(w, r.URL.Path, body)
	default:
		fail(w, http.StatusNotFound, "no such control")
	}
}

func main() {
	origin := flag.String("origin", "", "the member's bare origin repository")
	rs := &repos{home: "acme/member"}
	flag.Var(rs, "repo", "the owner/name GITHUB_REPOSITORY names; owner/name=DIR[;archived][;fork], repeatable, adds a fleet member served from DIR")
	token := flag.String("token", "", "the token cn must present")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready", "", "file to write the base URL to once listening")
	caOut := flag.String("ca-out", "", "file to write the certificate PEM to")
	licReady := flag.String("licstub-ready", "", "licstub's --ready file, read when a key dispatch is forwarded")
	licCA := flag.String("licstub-ca", "", "licstub's --ca-out file")
	agent := flag.String("agent", "", "the stub agent a routine fire runs on the item it names")
	routine := flag.String("routine-token", routineToken, "the bearer the routine fire route accepts")
	sess := defaultSession
	flag.Int64Var(&sess.UserID, "user-id", sess.UserID, "the id GET /user answers")
	flag.StringVar(&sess.UserLogin, "user", sess.UserLogin, "the login GET /user answers")
	flag.Int64Var(&sess.RepoID, "repo-id", sess.RepoID, "the repo's id")
	flag.Int64Var(&sess.OwnerID, "owner-id", sess.OwnerID, "the repo owner's id")
	flag.StringVar(&sess.OwnerType, "owner-type", sess.OwnerType, "User or Organization")
	flag.BoolVar(&sess.Private, "private", false, "the repo is private")
	flag.BoolVar(&sess.NoPush, "no-push", false, "the person lacks push access: key dispatches answer 403")
	flag.BoolVar(&sess.NoApp, "no-app", false, "no App is installed: key dispatches are accepted and no check run ever comes")
	flag.StringVar(&sess.EventName, "event-name", "", "the OIDC token's event_name (default workflow_dispatch)")
	flag.StringVar(&sess.WorkflowRef, "workflow-ref", "", "the OIDC token's job_workflow_ref (default the update workflow on main)")
	flag.Parse()
	if *origin == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "usage: ghstub --origin BARE.git --token T [--repo O/N] [--repo O/N=DIR]... [--ready F] [--ca-out F]")
		os.Exit(2)
	}
	cert, pemBytes, err := stubtls.SelfSigned("ghstub")
	if err != nil {
		die(err)
	}
	if *caOut != "" {
		if err := os.WriteFile(*caOut, pemBytes, 0o644); err != nil {
			die(err)
		}
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		die(err)
	}
	st := newStub(*origin, rs.home, *token)
	st.fleet = rs.members
	st.agent, st.routineToken = *agent, *routine
	st.sess, st.licReady, st.licCA = sess, *licReady, *licCA
	srv := &http.Server{Handler: st, ReadHeaderTimeout: 10 * time.Second}
	srv.TLSConfig = stubtls.Config(cert)
	if *ready != "" {
		if err := stubtls.WriteReady(*ready, "https://"+ln.Addr().String()); err != nil {
			die(err)
		}
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		_ = srv.Close()
	}()
	if err := srv.ServeTLS(ln, "", ""); err != nil && err != http.ErrServerClosed {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "ghstub: %v\n", err)
	os.Exit(1)
}
