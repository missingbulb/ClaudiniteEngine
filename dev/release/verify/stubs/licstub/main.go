// Command licstub stands in for the Claudinite license server's one route
// a fleet run asks (ClaudiniteLicenses workers/key), over HTTPS on
// loopback, for the rehearsal's fleet mode. It signs every key with a
// license issuing key it makes at start and certifies with --root-key
// (dev/release/keys/testkeys/root.key), so the roots cn embeds in a development build
// verify it.
//
//	POST /v1/actions-key   a key from ghstub's OIDC token, under the
//	                       Worker's pin rules, naming the owner's plan
//
// Control endpoints, unauthenticated:
//
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
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/entitlement"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
	"github.com/missingbulb/ClaudiniteEngine/dev/release/verify/stubs/stubtls"
)

// pinned are the workflows the Worker issues Actions keys to.
var pinned = []string{"claudinite-scheduler", "claudinite-executor", "claudinite-update"}

// config is how the stub answers; POST /_stub/config replaces the fields
// it names.
type config struct {
	// Plan overrides the plan; empty is public for a public repo and
	// private-repo for a private one, plans that hold no fleet.
	Plan string `json:"plan"`
	// Refuse is the reason the route refuses with; empty issues a key.
	Refuse        string `json:"refuse"`
	Down          bool   `json:"down"`
	DefaultBranch string `json:"default_branch"`
	OwnerType     string `json:"owner_type"`
	// OwnerLogin overrides the owner the key names, standing in for a
	// server that says the fleet belongs to another account.
	OwnerLogin string `json:"owner_login"`
}

type stub struct {
	mu      sync.Mutex
	cfg     config
	issuing ed25519.PrivateKey
	cert    sign.Certificate
	log     []string
	now     func() time.Time
}

func newStub(root ed25519.PrivateKey, cfg config, now func() time.Time) (*stub, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	cert, err := sign.Issue(root, pub, sign.UseLicense, now().Add(-time.Hour), now().AddDate(0, 0, 89))
	if err != nil {
		return nil, err
	}
	return &stub{cfg: cfg, issuing: priv, cert: cert, now: now}, nil
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

// identity is what a key binds: the repo and its owner.
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

// mint signs an Actions key for id.
func (s *stub) mint(id identity, ttl time.Duration) (string, error) {
	now := s.now()
	p := map[string]any{"v": 1, "typ": "actions", "kid": sign.KeyID(s.issuing.Public().(ed25519.PublicKey)),
		"repo_id": id.RepoID, "owner_id": id.OwnerID, "owner_type": id.OwnerType, "owner_login": id.OwnerLogin,
		"plan": s.plan(id), "state": "ok", "iat": now.Unix(), "exp": now.Add(ttl).Unix()}
	payload, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding
	key, err := json.Marshal(map[string]any{"certificate": s.cert, "payload": b64.EncodeToString(payload),
		"signature": b64.EncodeToString(ed25519.Sign(s.issuing, append([]byte(entitlement.KeyDomain), payload...)))})
	return string(key), err
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
	if r.Method == http.MethodPost && r.URL.Path == "/v1/actions-key" {
		s.actionsKey(w, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		return
	}
	reply(w, http.StatusNotFound, map[string]string{"error": "licstub does not answer " + r.Method + " " + r.URL.Path})
}

func (s *stub) control(w http.ResponseWriter, r *http.Request, body []byte) {
	switch r.URL.Path {
	case "/_stub/config":
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

// actionsKey is the Actions route. The token's signature is ghstub's and
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
	if str("aud") != entitlement.Audience {
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
	if why := s.cfg.Refuse; why != "" {
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
	if s.cfg.OwnerLogin != "" {
		id.OwnerLogin = s.cfg.OwnerLogin
	}
	key, err := s.mint(id, 6*time.Hour)
	if err != nil {
		refuse(w, http.StatusServiceUnavailable, "server-error")
		return
	}
	reply(w, 200, map[string]any{"key": key, "plan": s.plan(id)})
}

func main() {
	rootKey := flag.String("root-key", "dev/release/keys/testkeys/root.key", "the root that certifies the issuing key")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready", "", "file to write the base URL to once listening")
	caOut := flag.String("ca-out", "", "file to write the certificate PEM to")
	var cfg config
	flag.StringVar(&cfg.Plan, "plan", "", "the plan every key names (default by visibility)")
	flag.StringVar(&cfg.Refuse, "refuse", "", "the reason the route refuses with")
	flag.BoolVar(&cfg.Down, "down", false, "drop every request unanswered")
	flag.Parse()
	raw, err := os.ReadFile(*rootKey)
	if err != nil {
		die(err)
	}
	root, err := sign.ParsePrivateKey(string(raw))
	if err != nil {
		die(err)
	}
	st, err := newStub(root, cfg, time.Now)
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
