package main

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// The manager's git data API, over the bare origin: blobs, trees on a
// base tree, commits, and a branch's ref read, created and moved without
// forcing, as a fleet's mirror of the shelf writes its vendored branch.
var (
	gitRefPath    = regexp.MustCompile(`^/git/ref/heads/(.+)$`)
	gitCommitPath = regexp.MustCompile(`^/git/commits/([0-9a-f]+)$`)
)

// gitIn runs git on the origin with stdin and extra environment.
func (s *stub) gitIn(stdin []byte, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--git-dir", s.origin}, args...)...)
	cmd.Env = append(append(os.Environ(), "GIT_AUTHOR_NAME="+bot, "GIT_AUTHOR_EMAIL=41898282+github-actions[bot]@users.noreply.github.com",
		"GIT_COMMITTER_NAME=GitHub", "GIT_COMMITTER_EMAIL=noreply@github.com"), env...)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (s *stub) serveGitData(w http.ResponseWriter, r *http.Request, path string, body map[string]any) bool {
	str := func(k string) string { v, _ := body[k].(string); return v }
	switch {
	case r.Method == http.MethodGet && gitRefPath.MatchString(path):
		sha := s.headOf(gitRefPath.FindStringSubmatch(path)[1])
		if sha == "" {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		reply(w, 200, map[string]any{"object": map[string]string{"sha": sha, "type": "commit"}})
	case r.Method == http.MethodGet && gitCommitPath.MatchString(path):
		sha := gitCommitPath.FindStringSubmatch(path)[1]
		tree, err := s.git("rev-parse", "--verify", "--quiet", sha+"^{tree}")
		if err != nil {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		reply(w, 200, map[string]any{"sha": sha, "tree": map[string]string{"sha": tree}})
	case r.Method == http.MethodGet && treePath.MatchString(path) && r.URL.Query().Get("recursive") == "1":
		ref := treePath.FindStringSubmatch(path)[1]
		out, err := s.git("ls-tree", "-r", "-t", ref)
		if err != nil {
			fail(w, http.StatusNotFound, "Not Found")
			return true
		}
		tree := []map[string]string{}
		for _, l := range strings.Split(out, "\n") {
			meta, p, ok := strings.Cut(l, "\t")
			if f := strings.Fields(meta); ok && len(f) == 3 {
				tree = append(tree, map[string]string{"path": p, "type": f[1], "sha": f[2]})
			}
		}
		reply(w, 200, map[string]any{"tree": tree, "truncated": false})
	case r.Method == http.MethodPost && path == "/git/blobs":
		raw, err := base64.StdEncoding.DecodeString(str("content"))
		if err != nil || str("encoding") != "base64" {
			fail(w, http.StatusUnprocessableEntity, "content is not base64")
			return true
		}
		sha, err := s.gitIn(raw, nil, "hash-object", "-w", "--stdin")
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return true
		}
		s.calls = append(s.calls, "git blob")
		reply(w, http.StatusCreated, map[string]string{"sha": sha})
	case r.Method == http.MethodPost && path == "/git/trees":
		dir, err := os.MkdirTemp("", "ghstub-index-")
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return true
		}
		defer func() { _ = os.RemoveAll(dir) }()
		index := filepath.Join(dir, "index")
		env := []string{"GIT_INDEX_FILE=" + index}
		if base := str("base_tree"); base != "" {
			if _, err := s.gitIn(nil, env, "read-tree", base); err != nil {
				fail(w, http.StatusUnprocessableEntity, "base_tree "+base+" is not a tree")
				return true
			}
		}
		entries, _ := body["tree"].([]any)
		for _, e := range entries {
			m, _ := e.(map[string]any)
			mode, _ := m["mode"].(string)
			sha, _ := m["sha"].(string)
			p, _ := m["path"].(string)
			if _, err := s.gitIn(nil, env, "update-index", "--add", "--cacheinfo", mode+","+sha+","+p); err != nil {
				fail(w, http.StatusUnprocessableEntity, "bad tree entry "+p)
				return true
			}
		}
		sha, err := s.gitIn(nil, env, "write-tree")
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return true
		}
		reply(w, http.StatusCreated, map[string]string{"sha": sha})
	case r.Method == http.MethodPost && path == "/git/commits":
		args := []string{"commit-tree", str("tree"), "-m", str("message")}
		parents, _ := body["parents"].([]any)
		for _, p := range parents {
			args = append(args, "-p", p.(string))
		}
		sha, err := s.gitIn(nil, nil, args...)
		if err != nil {
			fail(w, http.StatusUnprocessableEntity, err.Error())
			return true
		}
		reply(w, http.StatusCreated, map[string]string{"sha": sha})
	case r.Method == http.MethodPost && path == "/git/refs":
		ref, sha := str("ref"), str("sha")
		branch, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok || s.headOf(branch) != "" {
			fail(w, http.StatusUnprocessableEntity, "Reference already exists")
			return true
		}
		if _, err := s.git("update-ref", ref, sha, ""); err != nil {
			fail(w, http.StatusUnprocessableEntity, err.Error())
			return true
		}
		s.calls = append(s.calls, "git ref "+branch)
		reply(w, http.StatusCreated, map[string]any{"ref": ref, "object": map[string]string{"sha": sha}})
	case r.Method == http.MethodPatch && refPath.MatchString(path):
		branch, sha := refPath.FindStringSubmatch(path)[1], str("sha")
		old := s.headOf(branch)
		if old == "" {
			fail(w, http.StatusUnprocessableEntity, "Reference does not exist")
			return true
		}
		if force, _ := body["force"].(bool); !force {
			if _, err := s.git("merge-base", "--is-ancestor", old, sha); err != nil {
				fail(w, http.StatusUnprocessableEntity, "Update is not a fast forward")
				return true
			}
		}
		if _, err := s.git("update-ref", "refs/heads/"+branch, sha, old); err != nil {
			fail(w, http.StatusUnprocessableEntity, err.Error())
			return true
		}
		s.calls = append(s.calls, "git ref "+branch)
		reply(w, 200, map[string]any{"ref": "refs/heads/" + branch, "object": map[string]string{"sha": sha}})
	default:
		return false
	}
	return true
}
