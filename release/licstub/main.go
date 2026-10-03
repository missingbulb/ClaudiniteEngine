// Command licstub stands in for the Claudinite license server
// (ClaudiniteLicenses workers/key) over HTTPS on loopback, for the
// rehearsal's license mode. It signs every key with a license issuing key
// it makes at start and certifies with --root-key (testkeys/root.key), so
// the roots cn embeds in a development build verify it.
//
// Routes, as the Worker answers them:
//
//	GET  /v1/login/config            the device flow's client id and ghstub's URLs
//	POST /v1/login/refresh           a fresh App user token for a refresh token
//	POST /v1/session-key             a desktop key; GET /user and the repo are
//	POST /v1/public/session-key      read from ghstub with the caller's token
//	POST /v1/actions-key             an Actions key from ghstub's OIDC token,
//	                                 under the Worker's pin rules
//	POST /v1/item-grant              a grant key for an Actions key and an issue
//
// Control endpoints, unauthenticated:
//
//	POST /_stub/webhook   ghstub forwards a claudinite-key dispatch here and
//	                      keeps the answer as the key check run
//	POST /_stub/config    replaces the config fields it names (config)
//	GET  /_stub/log       one line per request answered
//
// It writes its base URL to --ready once listening and the certificate to
// --ca-out.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/release/stubtls"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// Refusals is every reason the key Worker's README names on its paths.
// --refuse takes any reason, so a rehearsal can name one a later Worker
// adds.
var Refusals = []string{
	"token-missing", "token-invalid", "sender-not-user", "repo-not-visible", "no-push-access", "app-not-installed",
	"github-error", "server-error", "token-malformed", "token-unknown-key", "token-signature", "token-issuer",
	"token-audience", "token-expired", "token-not-yet-valid", "token-claims", "jwks-unavailable", "pull-request-trigger",
	"workflow-not-pinned", "repo-not-synced", "key-invalid", "key-not-actions", "issue-invalid", "rate-limited",
	"refresh-not-configured",
}

// pinned are the workflows the Worker issues Actions keys to.
var pinned = []string{"claudinite-scheduler", "claudinite-executor", "claudinite-update"}

// config is how the stub answers; POST /_stub/config replaces the fields
// it names.
type config struct {
	// Plan overrides the plan; empty is public for a public repo and
	// private-repo for a private one, as the Worker resolves it.
	Plan  string `json:"plan"`
	State string `json:"state"`
	// Refuse maps a path (web, desktop, actions, grant) to the reason it
	// refuses with.
	Refuse      map[string]string `json:"refuse"`
	DelayMS     int64             `json:"delay_ms"`
	Down        bool              `json:"down"`
	Release     *license.Release  `json:"release"`
	Features    []string          `json:"features"`
	Seats       *license.Seats    `json:"seats"`
	CheckoutURL string            `json:"checkout_url"`
	WrongNonce  bool              `json:"wrong_nonce"`
	// RejectToken is an App user token the desktop path answers 401
	// token-invalid to, so cn refreshes.
	RejectToken   string `json:"reject_token"`
	DefaultBranch string `json:"default_branch"`
	OwnerType     string `json:"owner_type"`
}

type stub struct {
	mu      sync.Mutex
	cfg     config
	issuing ed25519.PrivateKey
	cert    sign.Certificate
	root    ed25519.PublicKey
	gh      func() (string, *http.Client, error)
	log     []string
	now     func() time.Time
	refresh int
}

func newStub(root ed25519.PrivateKey, cfg config, gh func() (string, *http.Client, error), now func() time.Time) (*stub, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	cert, err := sign.Issue(root, pub, sign.UseLicense, now().Add(-time.Hour), now().AddDate(0, 0, 89))
	if err != nil {
		return nil, err
	}
	return &stub{cfg: cfg, issuing: priv, cert: cert, root: root.Public().(ed25519.PublicKey), gh: gh, now: now}, nil
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func refuse(w http.ResponseWriter, code int, reason string) {
	reply(w, code, map[string]string{"refused": reason})
}

// identity is what a key binds: the repo, its owner and visibility.
type identity struct {
	RepoID     int64
	OwnerID    int64
	OwnerType  string
	OwnerLogin string
	Private    bool
}

func (s *stub) plan(id identity) string {
	if s.cfg.Plan != "" {
		return s.cfg.Plan
	}
	if id.Private {
		return "private-repo"
	}
	return "public"
}

// mint signs a key of typ for id; user and nonce are omitted when zero.
func (s *stub) mint(typ string, id identity, user int64, nonce string, ttl time.Duration, issue int64) (string, string, string, error) {
	now := s.now()
	state := s.cfg.State
	if state == "" {
		state = "ok"
	}
	features := s.cfg.Features
	if features == nil {
		features = []string{"work-checks", "forced-skill-loading", "in-session-growth", "claudinite-tasks", "updates"}
		if p := s.plan(id); p == "personal" || p == "organization" || p == "internal" {
			features = append(features, "fleet")
		}
		if state == "degraded" {
			features = []string{}
		}
	}
	rel := license.Release{Held: []string{}, Revoked: []string{}, SecurityFixes: []string{}, PackKeys: []string{}}
	if s.cfg.Release != nil {
		rel = *s.cfg.Release
		for _, l := range []*[]string{&rel.Held, &rel.Revoked, &rel.SecurityFixes, &rel.PackKeys} {
			if *l == nil {
				*l = []string{}
			}
		}
	}
	p := map[string]any{"v": 1, "typ": typ, "kid": sign.KeyID(s.issuing.Public().(ed25519.PublicKey)),
		"repo_id": id.RepoID, "owner_id": id.OwnerID, "owner_type": id.OwnerType, "owner_login": id.OwnerLogin,
		"plan": s.plan(id), "iat": now.Unix(), "exp": now.Add(ttl).Unix(), "state": state, "grace_until": nil,
		"features": features, "release": rel, "seats": s.cfg.Seats, "checkout_url": nil, "portal_url": nil, "notice": nil}
	if state == "grace" {
		p["grace_until"] = now.Add(7 * 24 * time.Hour).Unix()
	}
	if s.cfg.CheckoutURL != "" {
		p["checkout_url"] = s.cfg.CheckoutURL
	}
	if user != 0 {
		p["user_id"] = user
	}
	if nonce != "" {
		p["nonce"] = nonce
	}
	if issue != 0 {
		p["issue"] = issue
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return "", "", "", err
	}
	b64 := base64.RawURLEncoding
	key, err := json.Marshal(map[string]any{"certificate": s.cert, "payload": b64.EncodeToString(payload),
		"signature": b64.EncodeToString(ed25519.Sign(s.issuing, append([]byte(license.KeyDomain), payload...)))})
	return string(key), s.plan(id), state, err
}

func (s *stub) logf(format string, a ...any) { s.log = append(s.log, fmt.Sprintf(format, a...)) }

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if strings.HasPrefix(r.URL.Path, "/_stub/") {
		s.control(w, r, body)
		return
	}
	s.logf("%s %s", r.Method, r.URL.Path)
	if s.cfg.Down {
		// The server is unreachable: drop the connection unanswered.
		if hj, ok := w.(http.Hijacker); ok {
			if c, _, err := hj.Hijack(); err == nil {
				_ = c.Close()
				return
			}
		}
		refuse(w, http.StatusServiceUnavailable, "server-error")
		return
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/login/config":
		base, _, err := s.gh()
		if err != nil {
			refuse(w, http.StatusServiceUnavailable, "server-error")
			return
		}
		reply(w, 200, map[string]string{"client_id": "Iv1.licstub", "device_code_url": base + "/login/device/code", "token_url": base + "/login/oauth/access_token"})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/login/refresh":
		var in struct {
			RefreshToken string `json:"refresh_token"`
		}
		if json.Unmarshal(body, &in) != nil || !strings.HasPrefix(in.RefreshToken, "ghr_") {
			reply(w, 200, map[string]string{"error": "bad_refresh_token"})
			return
		}
		s.refresh++
		s.logf("refreshed %d", s.refresh)
		reply(w, 200, map[string]any{"access_token": fmt.Sprintf("ghu_refreshed-%d", s.refresh), "expires_in": 28800,
			"refresh_token": fmt.Sprintf("ghr_refreshed-%d", s.refresh), "refresh_token_expires_in": 15897600})
	case r.Method == http.MethodPost && (r.URL.Path == "/v1/session-key" || r.URL.Path == "/v1/public/session-key"):
		s.sessionKey(w, bearer, body)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/actions-key":
		s.actionsKey(w, bearer)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/item-grant":
		s.itemGrant(w, bearer, body)
	default:
		reply(w, http.StatusNotFound, map[string]string{"error": "licstub does not answer " + r.Method + " " + r.URL.Path})
	}
}

func (s *stub) control(w http.ResponseWriter, r *http.Request, body []byte) {
	switch r.URL.Path {
	case "/_stub/webhook":
		s.webhook(w, body)
	case "/_stub/config":
		// A named release, feature list or seats replaces the old one
		// whole rather than merging into it; refuse merges per path.
		var named map[string]json.RawMessage
		_ = json.Unmarshal(body, &named)
		if _, ok := named["release"]; ok {
			s.cfg.Release = nil
		}
		if _, ok := named["features"]; ok {
			s.cfg.Features = nil
		}
		if _, ok := named["seats"]; ok {
			s.cfg.Seats = nil
		}
		if err := json.Unmarshal(body, &s.cfg); err != nil {
			reply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, s.cfg)
	case "/_stub/log":
		reply(w, 200, s.log)
	default:
		reply(w, http.StatusNotFound, map[string]string{"error": "no such control"})
	}
}

// webhook answers a forwarded claudinite-key dispatch with the check run
// the Worker would create.
func (s *stub) webhook(w http.ResponseWriter, body []byte) {
	var in struct {
		EventType     string `json:"event_type"`
		ClientPayload struct {
			Nonce string `json:"nonce"`
			Head  string `json:"head"`
		} `json:"client_payload"`
		Sender struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"sender"`
		Repository struct {
			ID      int64 `json:"id"`
			Private bool  `json:"private"`
			Owner   struct {
				ID    int64  `json:"id"`
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"owner"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		reply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logf("webhook %s nonce %s", in.EventType, in.ClientPayload.Nonce)
	ans := map[string]any{"delay_ms": s.cfg.DelayMS}
	if why := s.cfg.Refuse["web"]; why != "" {
		ans["title"], ans["summary"] = "Claudinite key refused", why+": licstub was told to refuse"
		reply(w, 200, ans)
		return
	}
	id := identity{RepoID: in.Repository.ID, OwnerID: in.Repository.Owner.ID, OwnerType: in.Repository.Owner.Type,
		OwnerLogin: in.Repository.Owner.Login, Private: in.Repository.Private}
	nonce := in.ClientPayload.Nonce
	if s.cfg.WrongNonce {
		nonce = "wrong-nonce-0123456789"
	}
	key, plan, state, err := s.mint("session", id, in.Sender.ID, nonce, 7*24*time.Hour, 0)
	if err != nil {
		reply(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ans["title"], ans["text"] = "Claudinite key", key
	ans["summary"] = fmt.Sprintf("%s key for @%s (sender type %s), state %s, issued %s", plan, in.Sender.Login, in.Sender.Type, state, s.now().UTC().Format(time.RFC3339))
	reply(w, 200, ans)
}

// sessionKey is the desktop path: the caller's App user token reads the
// user and the repo from ghstub.
func (s *stub) sessionKey(w http.ResponseWriter, bearer string, body []byte) {
	var in struct {
		Repo  string `json:"repo"`
		Nonce string `json:"nonce"`
	}
	if json.Unmarshal(body, &in) != nil || in.Repo == "" || in.Nonce == "" {
		refuse(w, http.StatusBadRequest, "body-invalid")
		return
	}
	if bearer == "" {
		refuse(w, http.StatusUnauthorized, "token-missing")
		return
	}
	if bearer == s.cfg.RejectToken {
		refuse(w, http.StatusUnauthorized, "token-invalid")
		return
	}
	if why := s.cfg.Refuse["desktop"]; why != "" {
		body := map[string]string{"refused": why}
		if s.cfg.CheckoutURL != "" {
			body["checkout_url"] = s.cfg.CheckoutURL
		}
		reply(w, http.StatusForbidden, body)
		return
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if code, err := s.ghGet("/user", bearer, &user); err != nil {
		if code == http.StatusUnauthorized {
			refuse(w, http.StatusUnauthorized, "token-invalid")
		} else {
			refuse(w, http.StatusBadGateway, "github-error")
		}
		return
	}
	if user.Type != "User" {
		refuse(w, http.StatusForbidden, "sender-not-user")
		return
	}
	var repo struct {
		ID      int64 `json:"id"`
		Private bool  `json:"private"`
		Owner   struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"owner"`
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if code, err := s.ghGet("/repos/"+in.Repo, bearer, &repo); err != nil {
		if code == http.StatusNotFound {
			refuse(w, http.StatusForbidden, "repo-not-visible")
		} else {
			refuse(w, http.StatusBadGateway, "github-error")
		}
		return
	}
	if !repo.Permissions.Push {
		refuse(w, http.StatusForbidden, "no-push-access")
		return
	}
	id := identity{RepoID: repo.ID, OwnerID: repo.Owner.ID, OwnerType: repo.Owner.Type, OwnerLogin: repo.Owner.Login, Private: repo.Private}
	nonce := in.Nonce
	if s.cfg.WrongNonce {
		nonce = "wrong-nonce-0123456789"
	}
	key, plan, state, err := s.mint("session", id, user.ID, nonce, 7*24*time.Hour, 0)
	if err != nil {
		refuse(w, http.StatusServiceUnavailable, "server-error")
		return
	}
	reply(w, 200, map[string]any{"key": key, "plan": plan, "state": state, "notice": nil, "checkout_url": nil, "portal_url": nil})
}

// actionsKey is the Actions path. The token's signature is ghstub's and
// is not checked; its claims are, under the Worker's pin rules.
func (s *stub) actionsKey(w http.ResponseWriter, bearer string) {
	if bearer == "" {
		refuse(w, http.StatusUnauthorized, "token-missing")
		return
	}
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 {
		refuse(w, http.StatusUnauthorized, "token-malformed")
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	var c map[string]any
	if err != nil || json.Unmarshal(raw, &c) != nil {
		refuse(w, http.StatusUnauthorized, "token-malformed")
		return
	}
	str := func(k string) string { v, _ := c[k].(string); return v }
	if str("aud") != license.Audience {
		refuse(w, http.StatusUnauthorized, "token-audience")
		return
	}
	for _, k := range []string{"repository_id", "repository_owner_id", "repository", "repository_owner", "repository_visibility", "event_name", "job_workflow_ref"} {
		if str(k) == "" {
			refuse(w, http.StatusUnauthorized, "token-claims")
			return
		}
	}
	if ev := str("event_name"); ev == "pull_request" || ev == "pull_request_target" {
		refuse(w, http.StatusForbidden, "pull-request-trigger")
		return
	}
	branch := s.cfg.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	ok := false
	for _, n := range pinned {
		if str("job_workflow_ref") == str("repository")+"/.github/workflows/"+n+".yml@refs/heads/"+branch {
			ok = true
		}
	}
	if !ok {
		refuse(w, http.StatusForbidden, "workflow-not-pinned")
		return
	}
	if why := s.cfg.Refuse["actions"]; why != "" {
		code := http.StatusForbidden
		if why == "server-error" {
			code = http.StatusServiceUnavailable
		}
		refuse(w, code, why)
		return
	}
	repoID, _ := strconv.ParseInt(str("repository_id"), 10, 64)
	ownerID, _ := strconv.ParseInt(str("repository_owner_id"), 10, 64)
	ownerType := s.cfg.OwnerType
	if ownerType == "" {
		ownerType = "User"
	}
	id := identity{RepoID: repoID, OwnerID: ownerID, OwnerType: ownerType, OwnerLogin: str("repository_owner"), Private: str("repository_visibility") != "public"}
	key, plan, state, err := s.mint("actions", id, 0, "", 6*time.Hour, 0)
	if err != nil {
		refuse(w, http.StatusServiceUnavailable, "server-error")
		return
	}
	reply(w, 200, map[string]any{"key": key, "plan": plan, "state": state, "notice": nil, "checkout_url": nil, "portal_url": nil})
}

// itemGrant exchanges an Actions key for a grant naming an issue.
func (s *stub) itemGrant(w http.ResponseWriter, bearer string, body []byte) {
	var in struct {
		Issue int64 `json:"issue"`
	}
	if json.Unmarshal(body, &in) != nil || in.Issue <= 0 {
		refuse(w, http.StatusBadRequest, "issue-invalid")
		return
	}
	roots := []ed25519.PublicKey{s.root}
	k, err := license.VerifyKey([]byte(bearer), roots, s.now())
	if err != nil {
		reply(w, http.StatusUnauthorized, map[string]string{"refused": "key-invalid", "reason": string(license.ReasonOf(err))})
		return
	}
	if k.Typ != "actions" {
		refuse(w, http.StatusForbidden, "key-not-actions")
		return
	}
	id := identity{RepoID: k.RepoID, OwnerID: k.OwnerID, OwnerType: k.OwnerType, OwnerLogin: k.OwnerLogin, Private: k.Plan != license.PlanPublic}
	key, _, _, err := s.mint("grant", id, 0, "", 6*time.Hour, in.Issue)
	if err != nil {
		refuse(w, http.StatusServiceUnavailable, "server-error")
		return
	}
	reply(w, 200, map[string]string{"grant": key})
}

// ghGet reads path from ghstub with the caller's token.
func (s *stub) ghGet(path, token string, out any) (int, error) {
	base, c, err := s.gh()
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return resp.StatusCode, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return 200, json.Unmarshal(b, out)
}

// ghFrom reads ghstub's base URL and certificate from its --ready and
// --ca-out files at each call.
func ghFrom(readyPath, caPath string) func() (string, *http.Client, error) {
	return func() (string, *http.Client, error) {
		base, err := os.ReadFile(readyPath)
		if err != nil {
			return "", nil, err
		}
		pemBytes, err := os.ReadFile(caPath)
		if err != nil {
			return "", nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return "", nil, errors.New("no certificate in " + caPath)
		}
		return strings.TrimSpace(string(base)), &http.Client{Timeout: 10 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}, nil
	}
}

// refuseFlag is --refuse PATH=REASON, repeatable.
type refuseFlag map[string]string

func (f refuseFlag) String() string { return fmt.Sprint(map[string]string(f)) }

func (f refuseFlag) Set(v string) error {
	path, reason, ok := strings.Cut(v, "=")
	if !ok || !slices.Contains([]string{"web", "desktop", "actions", "grant"}, path) || reason == "" {
		return errors.New("want web|desktop|actions|grant=REASON")
	}
	f[path] = reason
	return nil
}

func main() {
	rootKey := flag.String("root-key", "testkeys/root.key", "the root that certifies the issuing key")
	ghReady := flag.String("gh-ready", "", "ghstub's --ready file")
	ghCA := flag.String("gh-ca", "", "ghstub's --ca-out file")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready", "", "file to write the base URL to once listening")
	caOut := flag.String("ca-out", "", "file to write the certificate PEM to")
	cfg := config{Refuse: refuseFlag{}}
	flag.StringVar(&cfg.Plan, "plan", "", "the plan every key names (default by visibility)")
	flag.StringVar(&cfg.State, "state", "ok", "ok, grace, degraded or unverified")
	flag.Var(refuseFlag(cfg.Refuse), "refuse", "PATH=REASON: refuse that path (web, desktop, actions, grant)")
	flag.Int64Var(&cfg.DelayMS, "delay", 0, "milliseconds before a web key's check run shows")
	flag.BoolVar(&cfg.Down, "down", false, "drop every request unanswered")
	states := flag.String("states", "", "a JSON file holding the release block every key carries")
	features := flag.String("features", "", "comma-separated features (default the plan's)")
	seats := flag.String("seats", "", "PAID,COUNTED,HEADROOM")
	flag.StringVar(&cfg.CheckoutURL, "checkout-url", "", "the keys' checkout_url, and a desktop refusal's")
	flag.BoolVar(&cfg.WrongNonce, "wrong-nonce", false, "sign session keys with a nonce that is not the request's")
	flag.Parse()
	raw, err := os.ReadFile(*rootKey)
	if err != nil {
		die(err)
	}
	root, err := sign.ParsePrivateKey(string(raw))
	if err != nil {
		die(err)
	}
	if *states != "" {
		b, err := os.ReadFile(*states)
		if err != nil {
			die(err)
		}
		cfg.Release = &license.Release{}
		if err := json.Unmarshal(b, cfg.Release); err != nil {
			die(err)
		}
	}
	if *features != "" {
		cfg.Features = strings.Split(*features, ",")
	}
	if *seats != "" {
		var p, c, h int64
		if _, err := fmt.Sscanf(*seats, "%d,%d,%d", &p, &c, &h); err != nil {
			die(fmt.Errorf("--seats: %w", err))
		}
		cfg.Seats = &license.Seats{Paid: p, Counted: c, Headroom: h}
	}
	st, err := newStub(root, cfg, ghFrom(*ghReady, *ghCA), time.Now)
	if err != nil {
		die(err)
	}
	cert, pemBytes, err := stubtls.SelfSigned("licstub")
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
	// HTTP/1.1 only, so --down can drop a connection unanswered.
	srv := &http.Server{Handler: st, ReadHeaderTimeout: 10 * time.Second, TLSConfig: stubtls.Config(cert),
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}}
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
	fmt.Fprintf(os.Stderr, "licstub: %v\n", err)
	os.Exit(1)
}
