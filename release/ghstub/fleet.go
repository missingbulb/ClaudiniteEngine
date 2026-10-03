package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A fleet member is a repository the fleet token reaches beside the
// manager (--repo owner/name=DIR[;archived][;fork]): its default branch is
// the working tree at DIR, read as it stands, and its scheduler records a
// run per dispatch. Control endpoints for it:
//
//	POST /_stub/advance {"repo", "files": {path: text}, "run"}
//	                    writes the files into the member at its next
//	                    scheduler dispatch, as its own update would land
//	                    them; "run": false makes that dispatch start no run
//	POST /_stub/deny    {"repo"}: every later read of the member answers 403
type fleetMember struct {
	full, dir      string
	archived, fork bool
	deny           bool
	noRun          bool
	pending        map[string]string
	runs           []string
}

// repos is --repo, repeatable: a bare owner/name is the manager, an
// owner/name=DIR a fleet member.
type repos struct {
	home    string
	members []*fleetMember
}

func (r *repos) String() string { return r.home }

func (r *repos) Set(v string) error {
	full, rest, isMember := strings.Cut(v, "=")
	if !strings.Contains(full, "/") {
		return fmt.Errorf("%q is not owner/name", full)
	}
	if !isMember {
		r.home = full
		return nil
	}
	parts := strings.Split(rest, ";")
	m := &fleetMember{full: full, dir: parts[0]}
	for _, p := range parts[1:] {
		switch p {
		case "archived":
			m.archived = true
		case "fork":
			m.fork = true
		default:
			return fmt.Errorf("unknown member flag %q", p)
		}
	}
	r.members = append(r.members, m)
	return nil
}

var (
	memberContents   = regexp.MustCompile(`^/contents/(.+)$`)
	memberDispatches = regexp.MustCompile(`^/actions/workflows/([^/]+)/dispatches$`)
	memberRuns       = regexp.MustCompile(`^/actions/workflows/([^/]+)/runs$`)
)

func (s *stub) member(full string) *fleetMember {
	for _, m := range s.fleet {
		if strings.EqualFold(m.full, full) {
			return m
		}
	}
	return nil
}

// serveFleet answers the owner's repository listing and every call on a
// fleet member.
func (s *stub) serveFleet(w http.ResponseWriter, r *http.Request, body map[string]any) bool {
	path := r.URL.Path
	if path == "/user/repos" && r.Method == http.MethodGet {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			fail(w, http.StatusUnauthorized, "Bad credentials")
			return true
		}
		s.calls = append(s.calls, "user-repos")
		out := []map[string]any{}
		if pageOne(r) {
			out = append(out, wireRepo(s.repo, false, false))
			for _, m := range s.fleet {
				out = append(out, wireRepo(m.full, m.archived, m.fork))
			}
		}
		reply(w, 200, out)
		return true
	}
	rest, ok := strings.CutPrefix(path, "/repos/")
	if !ok {
		return false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return false
	}
	m := s.member(parts[0] + "/" + parts[1])
	if m == nil {
		return false
	}
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		fail(w, http.StatusUnauthorized, "Bad credentials")
		return true
	}
	if m.deny {
		s.calls = append(s.calls, "denied "+m.full)
		fail(w, http.StatusForbidden, "Resource not accessible by personal access token")
		return true
	}
	sub := ""
	if len(parts) == 3 {
		sub = "/" + parts[2]
	}
	switch {
	case r.Method == http.MethodGet && sub == "":
		reply(w, 200, wireRepo(m.full, m.archived, m.fork))
	case r.Method == http.MethodGet && memberContents.MatchString(sub):
		m.contents(w, memberContents.FindStringSubmatch(sub)[1])
	case r.Method == http.MethodPost && memberDispatches.MatchString(sub):
		s.dispatchMember(w, body, m, memberDispatches.FindStringSubmatch(sub)[1])
	case r.Method == http.MethodGet && memberRuns.MatchString(sub):
		runs := []map[string]string{}
		for i := len(m.runs) - 1; i >= 0; i-- {
			runs = append(runs, map[string]string{"created_at": m.runs[i], "event": "workflow_dispatch"})
		}
		reply(w, 200, map[string]any{"total_count": len(runs), "workflow_runs": runs})
	case r.Method == http.MethodGet && sub == "/commits":
		reply(w, 200, []any{})
	default:
		fail(w, http.StatusNotFound, "Not Found")
	}
	return true
}

func wireRepo(full string, archived, fork bool) map[string]any {
	owner, name, _ := strings.Cut(full, "/")
	return map[string]any{"name": name, "full_name": full, "owner": map[string]string{"login": owner},
		"archived": archived, "fork": fork, "default_branch": "main"}
}

func (m *fleetMember) contents(w http.ResponseWriter, rel string) {
	p := filepath.Join(m.dir, filepath.FromSlash(rel))
	if !strings.HasPrefix(p, filepath.Clean(m.dir)+string(filepath.Separator)) {
		fail(w, http.StatusNotFound, "Not Found")
		return
	}
	info, err := os.Stat(p)
	switch {
	case err != nil:
		fail(w, http.StatusNotFound, "Not Found")
	case info.IsDir():
		entries, _ := os.ReadDir(p)
		out := []map[string]string{}
		for _, e := range entries {
			kind := "file"
			if e.IsDir() {
				kind = "dir"
			}
			out = append(out, map[string]string{"name": e.Name(), "path": rel + "/" + e.Name(), "type": kind})
		}
		reply(w, 200, out)
	default:
		raw, err := os.ReadFile(p)
		if err != nil {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		reply(w, 200, map[string]string{"name": filepath.Base(rel), "path": rel, "sha": fmt.Sprintf("%x", len(raw)),
			"content": base64.StdEncoding.EncodeToString(raw), "encoding": "base64"})
	}
}

// dispatchMember is a member's scheduler dispatch: 404 where the tree has
// no such workflow, else the pending advance lands and a run starts.
func (s *stub) dispatchMember(w http.ResponseWriter, body map[string]any, m *fleetMember, wf string) {
	if _, err := os.Stat(filepath.Join(m.dir, ".github", "workflows", wf)); err != nil {
		fail(w, http.StatusNotFound, "Not Found")
		return
	}
	inputs, _ := body["inputs"].(map[string]any)
	wake, _ := inputs["wake"].(string)
	ref, _ := body["ref"].(string)
	s.disps = append(s.disps, dispatch{Workflow: wf, Ref: ref, Inputs: map[string]string{"wake": wake}, Repo: m.full})
	s.calls = append(s.calls, fmt.Sprintf("dispatch %s %s wake=%s", m.full, wf, wake))
	paths := make([]string, 0, len(m.pending))
	for p := range m.pending {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		dst := filepath.Join(m.dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.WriteFile(dst, []byte(m.pending[p]), 0o644); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	m.pending = nil
	if !m.noRun {
		m.runs = append(m.runs, time.Now().UTC().Format(time.RFC3339))
	}
	reply(w, http.StatusNoContent, nil)
}

func (s *stub) fleetControl(w http.ResponseWriter, path string, body map[string]any) {
	full, _ := body["repo"].(string)
	m := s.member(full)
	if m == nil {
		fail(w, http.StatusBadRequest, "no fleet member "+full)
		return
	}
	switch path {
	case "/_stub/advance":
		m.pending = map[string]string{}
		files, _ := body["files"].(map[string]any)
		for p, v := range files {
			m.pending[p], _ = v.(string)
		}
		run, ok := body["run"].(bool)
		m.noRun = ok && !run
	case "/_stub/deny":
		m.deny = true
	}
	reply(w, 200, map[string]string{})
}
