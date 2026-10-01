package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// session is what ghstub answers a session's key request and an Actions
// job's OIDC request with: the person, the repo, and how GitHub and the
// App behave. POST /_stub/session replaces any field it names.
type session struct {
	UserID      int64  `json:"user_id"`
	UserLogin   string `json:"user_login"`
	UserType    string `json:"user_type"`
	RepoID      int64  `json:"repo_id"`
	OwnerID     int64  `json:"owner_id"`
	OwnerType   string `json:"owner_type"`
	Private     bool   `json:"private"`
	NoPush      bool   `json:"no_push"`
	NoApp       bool   `json:"no_app"`
	EventName   string `json:"event_name"`
	WorkflowRef string `json:"workflow_ref"`
}

// checkRun is a key check run as licstub answered it, shown from visible on.
type checkRun struct {
	ID         int64
	ExternalID string
	Title      string
	Summary    string
	Text       string
	Visible    time.Time
}

var (
	repoCommitPath = regexp.MustCompile(`^/commits/([^/]+)$`)
	checkRunsPath  = regexp.MustCompile(`^/commits/([^/]+)/check-runs$`)
)

func (s *stub) owner() string { return strings.SplitN(s.repo, "/", 2)[0] }

// sessionAuth accepts the job token and any App user token (ghu_*), the
// shapes cn sends on the web and the desktop.
func (s *stub) sessionAuth(r *http.Request) bool {
	a := r.Header.Get("Authorization")
	return a == "Bearer "+s.token || strings.HasPrefix(a, "Bearer ghu_")
}

// serveSession answers the session and OIDC routes; it reports false for
// a path it does not own.
func (s *stub) serveSession(w http.ResponseWriter, r *http.Request, body map[string]any) bool {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/_oidc/token":
		s.oidc(w, r)
		return true
	case r.Method == http.MethodPost && path == "/login/device/code":
		s.calls = append(s.calls, "device-code")
		reply(w, 200, map[string]any{"device_code": "dc-stub", "user_code": "STUB-1234", "verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 1})
		return true
	case r.Method == http.MethodPost && path == "/login/oauth/access_token":
		q := r.URL.Query()
		if q.Get("device_code") != "dc-stub" || q.Get("client_id") == "" {
			reply(w, 200, map[string]string{"error": "incorrect_device_code"})
			return true
		}
		s.devicePolls++
		s.calls = append(s.calls, fmt.Sprintf("device-token %d", s.devicePolls))
		if s.devicePolls <= 2 {
			reply(w, 200, map[string]string{"error": "authorization_pending"})
			return true
		}
		s.devicePolls = 0
		reply(w, 200, map[string]any{"access_token": "ghu_login-1", "expires_in": 28800, "refresh_token": "ghr_login-1", "refresh_token_expires_in": 15897600})
		return true
	case r.Method == http.MethodGet && path == "/user":
		if !s.sessionAuth(r) {
			fail(w, http.StatusUnauthorized, "Bad credentials")
			return true
		}
		s.calls = append(s.calls, "user")
		reply(w, 200, map[string]any{"id": s.sess.UserID, "login": s.sess.UserLogin, "type": s.sess.UserType})
		return true
	}
	prefix := "/repos/" + s.repo
	if path != prefix && !strings.HasPrefix(path, prefix+"/") {
		return false
	}
	rest := strings.TrimPrefix(path, prefix)
	switch {
	case r.Method == http.MethodGet && rest == "":
	case r.Method == http.MethodGet && repoCommitPath.MatchString(rest):
	case r.Method == http.MethodGet && checkRunsPath.MatchString(rest):
	case r.Method == http.MethodPost && rest == "/dispatches":
	default:
		return false
	}
	if !s.sessionAuth(r) {
		fail(w, http.StatusUnauthorized, "Bad credentials")
		return true
	}
	switch {
	case rest == "":
		s.calls = append(s.calls, "repo")
		reply(w, 200, map[string]any{"id": s.sess.RepoID, "name": strings.SplitN(s.repo, "/", 2)[1], "full_name": s.repo,
			"private": s.sess.Private, "default_branch": "main",
			"owner":       map[string]any{"id": s.sess.OwnerID, "login": s.owner(), "type": s.sess.OwnerType},
			"permissions": map[string]bool{"push": !s.sess.NoPush}})
	case checkRunsPath.MatchString(rest):
		sha := checkRunsPath.FindStringSubmatch(rest)[1]
		s.calls = append(s.calls, "check-runs "+sha)
		out := []map[string]any{}
		for _, c := range s.checkRuns[sha] {
			if time.Now().Before(c.Visible) {
				continue
			}
			out = append(out, map[string]any{"id": c.ID, "name": "Claudinite key", "external_id": c.ExternalID, "status": "completed",
				"conclusion": "neutral", "output": map[string]string{"title": c.Title, "summary": c.Summary, "text": c.Text},
				"app": map[string]string{"slug": "claudinite"}})
		}
		reply(w, 200, map[string]any{"total_count": len(out), "check_runs": out})
	case repoCommitPath.MatchString(rest):
		ref, _ := url.PathUnescape(repoCommitPath.FindStringSubmatch(rest)[1])
		sha := s.headOf(ref)
		if sha == "" {
			fail(w, http.StatusUnprocessableEntity, "No commit found for SHA: "+ref)
			return true
		}
		reply(w, 200, map[string]string{"sha": sha})
	default:
		s.dispatchKey(w, r, body)
	}
	return true
}

// dispatchKey takes a claudinite-key repository_dispatch: refused without
// push access, accepted with no check run when no App is installed, and
// otherwise forwarded to licstub, whose answer becomes the check run.
func (s *stub) dispatchKey(w http.ResponseWriter, r *http.Request, body map[string]any) {
	ev, _ := body["event_type"].(string)
	payload, _ := body["client_payload"].(map[string]any)
	head, _ := payload["head"].(string)
	nonce, _ := payload["nonce"].(string)
	s.calls = append(s.calls, "repository-dispatch "+ev)
	if s.sess.NoPush {
		fail(w, http.StatusForbidden, "Resource not accessible by integration")
		return
	}
	if ev != "claudinite-key" && ev != "claudinite-key-public" {
		fail(w, http.StatusUnprocessableEntity, "unknown event_type "+ev)
		return
	}
	reply(w, http.StatusNoContent, nil)
	if s.sess.NoApp {
		return
	}
	hook := map[string]any{"event_type": ev, "client_payload": payload,
		"sender":     map[string]any{"id": s.sess.UserID, "login": s.sess.UserLogin, "type": s.sess.UserType},
		"repository": map[string]any{"id": s.sess.RepoID, "full_name": s.repo, "private": s.sess.Private, "owner": map[string]any{"id": s.sess.OwnerID, "login": s.owner(), "type": s.sess.OwnerType}}}
	var ans struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
		DelayMS int64  `json:"delay_ms"`
	}
	if err := s.licstub("/_stub/webhook", hook, &ans); err != nil {
		s.calls = append(s.calls, "webhook-failed "+err.Error())
		return
	}
	s.runID++
	s.checkRuns[head] = append(s.checkRuns[head], checkRun{ID: s.runID, ExternalID: nonce, Title: ans.Title, Summary: ans.Summary, Text: ans.Text,
		Visible: time.Now().Add(time.Duration(ans.DelayMS) * time.Millisecond)})
}

// licstub posts to the license stub named by --licstub-ready, trusting
// --licstub-ca; both are read at the call, since licstub starts after.
func (s *stub) licstub(path string, in, out any) error {
	base, err := os.ReadFile(s.licReady)
	if err != nil {
		return err
	}
	pemBytes, err := os.ReadFile(s.licCA)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pemBytes)
	c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	raw, _ := json.Marshal(in)
	resp, err := c.Post(strings.TrimSpace(string(base))+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", resp.Status, b)
	}
	return json.Unmarshal(b, out)
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
