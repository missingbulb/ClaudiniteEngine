package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// session is who the stub's repo belongs to and how an Actions job's OIDC
// token describes it. POST /_stub/session replaces any field it names.
type session struct {
	UserLogin   string `json:"user_login"`
	RepoID      int64  `json:"repo_id"`
	OwnerID     int64  `json:"owner_id"`
	Private     bool   `json:"private"`
	EventName   string `json:"event_name"`
	WorkflowRef string `json:"workflow_ref"`
}

var repoCommitPath = regexp.MustCompile(`^/commits/([^/]+)$`)

func (s *stub) owner() string { return strings.SplitN(s.repo, "/", 2)[0] }

// serveSession answers the OIDC route and a commit read; it reports false
// for a path it does not own.
func (s *stub) serveSession(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet && r.URL.Path == "/_oidc/token" {
		s.oidc(w, r)
		return true
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/repos/"+s.repo)
	if r.Method != http.MethodGet || !ok || !repoCommitPath.MatchString(rest) {
		return false
	}
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		fail(w, http.StatusUnauthorized, "Bad credentials")
		return true
	}
	ref, _ := url.PathUnescape(repoCommitPath.FindStringSubmatch(rest)[1])
	sha := s.headOf(ref)
	if sha == "" {
		sha = ref
	}
	c, found := s.commit(sha)
	if !found {
		fail(w, http.StatusUnprocessableEntity, "No commit found for SHA: "+ref)
		return true
	}
	reply(w, 200, c)
	return true
}

// oidc answers an Actions job's token request: an RS256 token carrying
// the claims the key Worker reads.
func (s *stub) oidc(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "bearer oidc-request-token" {
		fail(w, http.StatusUnauthorized, "Bad credentials")
		return
	}
	aud := r.URL.Query().Get("audience")
	s.calls = append(s.calls, "oidc "+aud)
	vis := "public"
	if s.sess.Private {
		vis = "private"
	}
	ref := s.sess.WorkflowRef
	if ref == "" {
		ref = s.repo + "/.github/workflows/claudinite-update.yml@refs/heads/main"
	}
	ev := s.sess.EventName
	if ev == "" {
		ev = "workflow_dispatch"
	}
	now := time.Now().Unix()
	claims := map[string]any{"iss": "https://token.actions.githubusercontent.com", "aud": aud, "sub": "repo:" + s.repo + ":ref:refs/heads/main",
		"iat": now, "nbf": now, "exp": now + 300, "jti": fmt.Sprintf("jti-%d", now),
		"repository": s.repo, "repository_owner": s.owner(), "repository_id": fmt.Sprint(s.sess.RepoID), "repository_owner_id": fmt.Sprint(s.sess.OwnerID),
		"repository_visibility": vis, "event_name": ev, "job_workflow_ref": ref, "ref": "refs/heads/main"}
	b64 := base64.RawURLEncoding
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "ghstub"})
	body, _ := json.Marshal(claims)
	signing := b64.EncodeToString(hdr) + "." + b64.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.rsaKey, crypto.SHA256, sum[:])
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, 200, map[string]string{"value": signing + "." + b64.EncodeToString(sig)})
}
