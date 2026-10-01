// Command ghstub stands in for the GitHub REST calls the updater makes
// (shared/githubapi), over HTTPS on loopback, for the rehearsal's update
// mode and the release workflow's hop job. Pull requests read their head
// from --origin, a bare repository, and a merge squashes onto its main
// there, as GitHub would. cn reaches it through CLAUDINITE_GITHUB_API.
//
// Control endpoints, unauthenticated, for the script driving it:
//
//	POST /_stub/run       {"sha"|"ref", "event", "status", "conclusion"}
//	                      adds a claudinite-ci.yml run (event push,
//	                      status completed by default)
//	POST /_stub/dispatch  {"conclusion"}: every later workflow_dispatch
//	                      adds a run with it on the ref's head; "" adds none
//	GET  /_stub/state     pulls, issues, dispatches and the call log
//
// It writes its base URL to --ready once listening and the certificate to
// --ca-out.
package main

import (
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
}

type issue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

type run struct {
	ID         int64  `json:"id"`
	HeadSHA    string `json:"head_sha"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	CreatedAt  string `json:"created_at"`
}

type dispatch struct {
	Workflow string            `json:"workflow"`
	Ref      string            `json:"ref"`
	Inputs   map[string]string `json:"inputs"`
}

// stubState is what GET /_stub/state answers.
type stubState struct {
	Pulls      []pull     `json:"pulls"`
	Issues     []issue    `json:"issues"`
	Dispatches []dispatch `json:"dispatches"`
	Calls      []string   `json:"calls"`
}

type stub struct {
	mu       sync.Mutex
	origin   string
	repo     string
	token    string
	next     int
	runID    int64
	clock    time.Time
	pulls    []*pull
	issues   []*issue
	runs     []run
	onDisp   string
	disps    []dispatch
	calls    []string
	comments map[int][]string
}

func newStub(origin, repo, token string) http.Handler {
	return &stub{origin: origin, repo: repo, token: token, next: 1, clock: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), comments: map[int][]string{}}
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
	s.runs = append(s.runs, run{ID: s.runID, HeadSHA: sha, Event: event, Status: status, Conclusion: conclusion, CreatedAt: s.clock.Format(time.RFC3339)})
}

func (s *stub) wire(p *pull) map[string]any {
	labels := []map[string]string{}
	for _, l := range p.Labels {
		labels = append(labels, map[string]string{"name": l})
	}
	return map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "merged": p.Merged,
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

	if strings.HasPrefix(r.URL.Path, "/_stub/") {
		s.control(w, r, str)
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
		sha := r.URL.Query().Get("head_sha")
		var out []run
		for i := len(s.runs) - 1; i >= 0; i-- {
			if s.runs[i].HeadSHA == sha {
				out = append(out, s.runs[i])
			}
		}
		reply(w, 200, map[string]any{"total_count": len(out), "workflow_runs": out})
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
		s.disps = append(s.disps, dispatch{wf, str("ref"), inputs})
		s.calls = append(s.calls, fmt.Sprintf("dispatch %s %s pr=%s", wf, str("ref"), inputs["pr"]))
		if s.onDisp != "" {
			s.addRun(head, "workflow_dispatch", "completed", s.onDisp)
		}
		reply(w, http.StatusNoContent, nil)
	case r.Method == http.MethodGet && path == "/pulls":
		out := []map[string]any{}
		for _, p := range s.pulls {
			if p.State == "open" {
				out = append(out, s.wire(p))
			}
		}
		reply(w, 200, out)
	case r.Method == http.MethodPost && path == "/pulls":
		if s.headOf(str("head")) == "" || s.headOf(str("base")) == "" {
			fail(w, http.StatusUnprocessableEntity, "Validation Failed: head or base does not exist")
			return
		}
		p := &pull{Number: s.next, Title: str("title"), Body: str("body"), Head: str("head"), Base: str("base"), State: "open", Labels: []string{}}
		s.next++
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
			s.calls = append(s.calls, fmt.Sprintf("close-pull %d", p.Number))
		}
		reply(w, 200, s.wire(p))
	case r.Method == http.MethodPut && mergePath.MatchString(path):
		s.merge(w, s.find(num(mergePath)), str("sha"), str("commit_title"), str("merge_method"))
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
		if p := s.find(n); p != nil {
			p.Labels = append(p.Labels, labels...)
		} else if is := s.findIssue(n); is != nil {
			is.Labels = append(is.Labels, labels...)
		} else {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		s.calls = append(s.calls, fmt.Sprintf("label %d %s", n, strings.Join(labels, ",")))
		reply(w, 200, []any{})
	case r.Method == http.MethodPost && commentsPath.MatchString(path):
		n := num(commentsPath)
		s.comments[n] = append(s.comments[n], str("body"))
		s.calls = append(s.calls, fmt.Sprintf("comment %d", n))
		reply(w, http.StatusCreated, map[string]any{"id": len(s.calls)})
	case r.Method == http.MethodGet && path == "/issues":
		label := r.URL.Query().Get("labels")
		out := []map[string]any{}
		for _, is := range s.issues {
			if label == "" || contains(is.Labels, label) {
				out = append(out, map[string]any{"number": is.Number, "title": is.Title, "body": is.Body})
			}
		}
		for _, p := range s.pulls {
			if p.State == "open" && (label == "" || contains(p.Labels, label)) {
				out = append(out, map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "pull_request": map[string]string{}})
			}
		}
		reply(w, 200, out)
	case r.Method == http.MethodPost && path == "/issues":
		is := &issue{Number: s.next, Title: str("title"), Body: str("body"), Labels: []string{}}
		if ls, ok := body["labels"].([]any); ok {
			for _, l := range ls {
				if v, ok := l.(string); ok {
					is.Labels = append(is.Labels, v)
				}
			}
		}
		s.next++
		s.issues = append(s.issues, is)
		s.calls = append(s.calls, fmt.Sprintf("create-issue %d %s", is.Number, is.Title))
		reply(w, http.StatusCreated, map[string]any{"number": is.Number})
	case r.Method == http.MethodPatch && issuePath.MatchString(path):
		is := s.findIssue(num(issuePath))
		if is == nil {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		is.Body = str("body")
		s.calls = append(s.calls, fmt.Sprintf("update-issue %d", is.Number))
		reply(w, 200, map[string]any{"number": is.Number})
	default:
		fail(w, http.StatusNotFound, "ghstub does not answer "+r.Method+" "+path)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *stub) findIssue(n int) *issue {
	for _, is := range s.issues {
		if is.Number == n {
			return is
		}
	}
	return nil
}

// merge squashes the PR's head onto its base in origin, refusing a closed
// PR or a head that moved from sha, as GitHub does.
func (s *stub) merge(w http.ResponseWriter, p *pull, sha, title, method string) {
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
	tree, err := s.git("rev-parse", sha+"^{tree}")
	if err == nil {
		var commit string
		commit, err = s.git("commit-tree", tree, "-p", base, "-m", title)
		if err == nil {
			_, err = s.git("update-ref", "refs/heads/"+p.Base, commit, base)
		}
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "squash: "+err.Error())
		return
	}
	p.State, p.Merged = "closed", true
	s.calls = append(s.calls, fmt.Sprintf("merge %d %s", p.Number, sha))
	reply(w, 200, map[string]any{"merged": true})
}

func (s *stub) control(w http.ResponseWriter, r *http.Request, str func(string) string) {
	switch r.URL.Path {
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
		st := stubState{Dispatches: s.disps, Calls: s.calls}
		for _, p := range s.pulls {
			st.Pulls = append(st.Pulls, *p)
		}
		for _, is := range s.issues {
			st.Issues = append(st.Issues, *is)
		}
		reply(w, 200, st)
	default:
		fail(w, http.StatusNotFound, "no such control")
	}
}

func main() {
	origin := flag.String("origin", "", "the member's bare origin repository")
	repo := flag.String("repo", "acme/member", "the owner/name GITHUB_REPOSITORY names")
	token := flag.String("token", "", "the token cn must present")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready", "", "file to write the base URL to once listening")
	caOut := flag.String("ca-out", "", "file to write the certificate PEM to")
	flag.Parse()
	if *origin == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "usage: ghstub --origin BARE.git --token T [--repo O/N] [--ready F] [--ca-out F]")
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
	srv := &http.Server{Handler: newStub(*origin, *repo, *token), ReadHeaderTimeout: 10 * time.Second}
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
