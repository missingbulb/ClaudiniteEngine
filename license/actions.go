package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
)

// Audience is the OIDC audience the license server accepts.
const Audience = "claudinite"

// ActionsResult is an Actions run's key, or why it holds none. A key whose
// state is degraded is still a key: the caller decides what it skips.
type ActionsResult struct {
	Key    *KeyPayload
	Cause  Cause
	Detail string
	Link   string
}

// RequestActions exchanges the job's OIDC token for the run's key and
// binds it to the repo the job runs in: GITHUB_REPOSITORY_ID,
// GITHUB_REPOSITORY_OWNER_ID and the event payload's repository at
// GITHUB_EVENT_PATH (visibility and owner type), falling back to the OIDC
// token's own repository_visibility claim when the payload names no
// repository. An Actions key is never cached and never fails open.
func RequestActions(w Worker, h *http.Client, getenv func(string) string, roots []ed25519.PublicKey, now func() time.Time, engine string) ActionsResult {
	tok, err := githubapi.OIDCToken(h, getenv, Audience)
	if errors.Is(err, githubapi.ErrNoOIDC) {
		return ActionsResult{Cause: CauseNoOIDC, Detail: "id-token: write is missing"}
	}
	if err != nil {
		return ActionsResult{Cause: CauseGitHubUnreachable, Detail: err.Error()}
	}
	repo, err := actionsRepo(getenv, tok)
	if err != nil {
		return ActionsResult{Cause: CauseActionsEnv, Detail: err.Error()}
	}
	ans, err := w.ActionsKey(tok, engine)
	var ref *licenseapi.Refusal
	switch {
	case err == nil:
	case errors.As(err, &ref) && ref.Reason != "" && ref.Status < 500:
		c := Cause(ref.Reason)
		return ActionsResult{Cause: c, Link: LinkFor(c)}
	default:
		return ActionsResult{Cause: CauseServerUnreachable, Detail: err.Error()}
	}
	k, err := VerifyKey([]byte(ans.Key), roots, now())
	if err != nil {
		return ActionsResult{Cause: CauseKeyRefused, Detail: string(ReasonOf(err))}
	}
	if c := Bind(k, repo, nil); c != "" {
		return ActionsResult{Cause: c, Detail: bindDetail(c, k, repo)}
	}
	return ActionsResult{Key: &k}
}

// actionsRepo is the job's repository as the runner describes it.
func actionsRepo(getenv func(string) string, oidc string) (RepoIdentity, error) {
	id, err1 := strconv.ParseInt(getenv("GITHUB_REPOSITORY_ID"), 10, 64)
	owner, err2 := strconv.ParseInt(getenv("GITHUB_REPOSITORY_OWNER_ID"), 10, 64)
	if err1 != nil || err2 != nil {
		return RepoIdentity{}, errors.New("GITHUB_REPOSITORY_ID and GITHUB_REPOSITORY_OWNER_ID must be set")
	}
	r := RepoIdentity{ID: id, OwnerID: owner, OwnerLogin: getenv("GITHUB_REPOSITORY_OWNER")}
	if ev, err := githubapi.ReadEvent(getenv("GITHUB_EVENT_PATH")); err == nil && ev.ID == id {
		r.Private, r.OwnerType = ev.Private, ev.Owner.Type
		return r, nil
	}
	vis, ok := tokenClaim(oidc, "repository_visibility")
	if !ok {
		return RepoIdentity{}, errors.New("neither the event payload nor the OIDC token says the repository's visibility")
	}
	r.Private = vis != "public"
	return r, nil
}

// tokenClaim reads one string claim of a JWT the runner just handed this
// job over TLS; the license server verifies the signature, so this does not.
func tokenClaim(jwt, name string) (string, bool) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims map[string]any
	if json.Unmarshal(raw, &claims) != nil {
		return "", false
	}
	v, ok := claims[name].(string)
	return v, ok
}
