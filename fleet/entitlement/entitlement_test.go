package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

var testNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func seeded(b byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = b
	return ed25519.NewKeyFromSeed(seed)
}

var testRoot = seeded(1)

func testRoots() []ed25519.PublicKey {
	return []ed25519.PublicKey{testRoot.Public().(ed25519.PublicKey)}
}

// mint signs an Actions key for repo 11, owned by acme (owner 3), on plan,
// with edits applied, under a license certificate from testRoot.
func mint(t *testing.T, plan string, edits map[string]any) string {
	t.Helper()
	p := map[string]any{"v": 1, "typ": "actions", "repo_id": 11, "owner_id": 3, "owner_type": "Organization", "owner_login": "Acme",
		"plan": plan, "iat": testNow.Unix(), "exp": testNow.Add(time.Hour).Unix(), "state": "ok"}
	for k, v := range edits {
		p[k] = v
	}
	issuing := seeded(9)
	pub := issuing.Public().(ed25519.PublicKey)
	cert, err := sign.Issue(testRoot, pub, sign.UseLicense, testNow.Add(-time.Hour), testNow.AddDate(0, 0, 80))
	if err != nil {
		t.Fatal(err)
	}
	p["kid"] = sign.KeyID(pub)
	payload, _ := json.Marshal(p)
	b64 := base64.RawURLEncoding
	key, _ := json.Marshal(map[string]any{"certificate": cert, "payload": b64.EncodeToString(payload),
		"signature": b64.EncodeToString(ed25519.Sign(issuing, append([]byte(KeyDomain), payload...)))})
	return string(key)
}

// server answers ActionsKey with a fixed key or error, recording the
// bearer it was asked with.
type server struct {
	key    string
	err    error
	bearer []string
}

func (s *server) ActionsKey(oidc, engine string) (licenseapi.KeyAnswer, error) {
	s.bearer = append(s.bearer, oidc)
	if s.err != nil {
		return licenseapi.KeyAnswer{}, s.err
	}
	return licenseapi.KeyAnswer{Key: s.key}, nil
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

var inActions = map[string]string{"GITHUB_REPOSITORY_ID": "11"}

func check(t *testing.T, s KeyServer, getenv func(string) string) Verdict {
	t.Helper()
	return Check(In{Server: s, Getenv: getenv, Roots: testRoots(), Now: testNow, Engine: "1.0.0",
		OIDC: func() (string, error) { return "oidc-token", nil }})
}

func TestAFleetPlanOwnersReposRun(t *testing.T) {
	for _, plan := range []string{"personal", "organization"} {
		s := &server{key: mint(t, plan, nil)}
		v := check(t, s, env(inActions))
		if v.Refused || v.Unverified {
			t.Fatalf("%s: %+v", plan, v)
		}
		if !v.Allows("acme/one", 0) || !v.Allows("ACME/two", 3) {
			t.Errorf("%s: the owner's repos are refused: %+v", plan, v)
		}
		if len(s.bearer) != 1 || s.bearer[0] != "oidc-token" {
			t.Errorf("%s: asked with %v, want the job's OIDC token alone", plan, s.bearer)
		}
	}
}

func TestAnotherOwnersRepoIsRefused(t *testing.T) {
	v := check(t, &server{key: mint(t, "organization", nil)}, env(inActions))
	if v.Allows("someone-else/repo", 0) {
		t.Error("a repo another account owns is allowed")
	}
	if v.Allows("acme-renamed/repo", 3) != true {
		t.Error("a repo whose owner id is the key's is refused for its login alone")
	}
	if v.Allows("acme/repo", 4) {
		t.Error("a repo whose owner id is not the key's is allowed for its login alone")
	}
	n := v.RepoNotice("someone-else/repo")
	if !strings.Contains(n, "someone-else/repo") || !strings.Contains(n, "Acme") {
		t.Errorf("notice %q names neither the repo nor the paying owner", n)
	}
}

func TestAnUnreachableServerFailsOpen(t *testing.T) {
	for name, in := range map[string]In{
		"server down": {Server: &server{err: errors.New("dial tcp: connection refused")}, OIDC: func() (string, error) { return "t", nil }},
		"server 503":  {Server: &server{err: &licenseapi.Refusal{Status: 503}}, OIDC: func() (string, error) { return "t", nil }},
		"no client":   {OIDC: func() (string, error) { return "t", nil }},
		"no OIDC":     {Server: &server{key: mint(t, "personal", nil)}, OIDC: func() (string, error) { return "", githubapi.ErrNoOIDC }},
		"GitHub down": {Server: &server{key: mint(t, "personal", nil)}, OIDC: func() (string, error) { return "", errors.New("OIDC token: 502 Bad Gateway") }},
	} {
		in.Getenv, in.Roots, in.Now = env(inActions), testRoots(), testNow
		v := Check(in)
		if v.Refused || !v.Unverified {
			t.Errorf("%s: %+v, want an unverified run", name, v)
			continue
		}
		if !v.Allows("anyone/repo", 0) {
			t.Errorf("%s: an unverified run refuses a repo", name)
		}
		if !strings.Contains(v.Notice, "unverified") {
			t.Errorf("%s: notice %q does not say the run is unverified", name, v.Notice)
		}
	}
}

func TestANonFleetPlanIsRefused(t *testing.T) {
	for _, plan := range []string{"public", "private-repo", "internal"} {
		v := check(t, &server{key: mint(t, plan, nil)}, env(inActions))
		if !v.Refused || v.Allows("acme/one", 3) {
			t.Errorf("%s: %+v, want a refused run", plan, v)
		}
		if !strings.Contains(v.Notice, plan) {
			t.Errorf("%s: notice %q does not name the plan", plan, v.Notice)
		}
	}
}

func TestAnAnswerThatIsNotAFleetKeyIsRefused(t *testing.T) {
	other := seeded(5)
	for name, s := range map[string]*server{
		"refusal":       {err: &licenseapi.Refusal{Status: 403, Reason: "no-plan"}},
		"expired":       {key: mint(t, "personal", map[string]any{"exp": testNow.Add(-time.Minute).Unix()})},
		"another repo":  {key: mint(t, "personal", map[string]any{"repo_id": 12})},
		"tampered plan": {key: strings.Replace(mint(t, "public", nil), `"payload":"`, `"payload":"x`, 1)},
	} {
		v := check(t, s, env(inActions))
		if !v.Refused || v.Allows("acme/one", 3) {
			t.Errorf("%s: %+v, want a refused run", name, v)
		}
	}
	untrusted := Check(In{Server: &server{key: mint(t, "personal", nil)}, Getenv: env(inActions), Now: testNow,
		Roots: []ed25519.PublicKey{other.Public().(ed25519.PublicKey)}, OIDC: func() (string, error) { return "t", nil }})
	if !untrusted.Refused {
		t.Errorf("a key under an untrusted root: %+v", untrusted)
	}
}
