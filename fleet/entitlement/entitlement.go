// Package entitlement is the fleet's one license check: whether the owner
// of the repos a fleet run reaches holds a fleet plan. A single repository
// needs no license and nothing else in the engine asks.
//
// At the start of a run the job exchanges its GitHub Actions OIDC token
// (audience "claudinite", which needs permissions: id-token: write) for the
// manager repository's Actions key at the license server's POST
// /v1/actions-key. The key is verified against the embedded roots and read
// for its plan and its owner; nothing about the person running the job is
// sent or read. The run is entitled when the plan is personal,
// organization or internal, and then reaches only the repos that owner
// owns. A license server that does not answer, or answers 5xx, fails
// open: the run goes on, unverified, and says so. A job without an OIDC
// token is refused, and a 408, 413 or 429 is the run's error.
package entitlement

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// Audience is the OIDC audience the license server accepts.
const Audience = "claudinite"

// KeyDomain prefixes the payload bytes a key's signature covers.
const KeyDomain = "claudinite-license-v1\n"

// FleetPlans are the plans that run a fleet: Personal, per personal
// GitHub account, Organization, per user, and Internal, the license
// server's plan for Claudinite's own account.
var FleetPlans = []string{"personal", "organization", "internal"}

// KeyServer is the license server's Actions key route.
type KeyServer interface {
	ActionsKey(oidcToken, engine string) (licenseapi.KeyAnswer, error)
}

// In is one check's reach. Server is nil when no client could be built;
// OIDC fetches the job's token for Audience; Getenv reads
// GITHUB_REPOSITORY_ID, the manager repository the key must name.
type In struct {
	Server KeyServer
	OIDC   func() (string, error)
	Getenv func(string) string
	Roots  []ed25519.PublicKey
	Now    time.Time
	Engine string
}

// Verdict is a run's entitlement. A refused run reaches nothing; an
// unverified one reaches every repo it would have; otherwise it reaches
// the repos OwnerLogin (OwnerID) owns.
type Verdict struct {
	Refused, Unverified bool
	// Transient is a refusal the server may lift on a retry: the run's
	// error, reaching nothing, with nothing for a person to do.
	Transient  bool
	Plan       string
	OwnerID    int64
	OwnerLogin string
	// Notice is the one sentence a refused or unverified run prints.
	Notice string
}

// Allows reports whether the run may reach repo (owner/name), whose owner's
// id is ownerID, 0 when unknown: the owner id decides where both are known,
// the login otherwise.
func (v Verdict) Allows(repo string, ownerID int64) bool {
	switch {
	case v.Refused:
		return false
	case v.Unverified:
		return true
	case ownerID != 0 && v.OwnerID != 0:
		return ownerID == v.OwnerID
	}
	owner, _, _ := strings.Cut(repo, "/")
	return strings.EqualFold(owner, v.OwnerLogin)
}

// RepoNotice is the line a run prints for a repo it does not reach.
func (v Verdict) RepoNotice(repo string) string {
	if v.Refused {
		return v.Notice
	}
	return fmt.Sprintf("[cn] fleet: %s is not reached: the fleet's %s plan covers the repos %s owns, and another account owns this one", repo, v.Plan, v.OwnerLogin)
}

func unverified(why string) Verdict {
	return Verdict{Unverified: true, Notice: "[cn] fleet: this run is unverified (" + why + "); it reaches every repo as if the owner held a fleet plan"}
}

func refused(why string) Verdict {
	return Verdict{Refused: true, Notice: "[cn] fleet: refused: " + why + "; a fleet needs the Personal or Organization plan on the account that owns its repos"}
}

// transient are the refusals the server may lift on a retry.
var transient = []int{408, 413, 429}

// Check asks the license server once for the run's key and judges it. Only
// the server's own silence (no answer, or a 5xx) fails open; what the
// fleet's owner controls, the job's permissions and environment, refuses.
func Check(in In) Verdict {
	if in.Server == nil {
		return refused("no license server client")
	}
	tok, err := in.OIDC()
	if errors.Is(err, githubapi.ErrNoOIDC) {
		if in.Getenv("GITHUB_ACTIONS") == "true" {
			return refused("the job has no Actions OIDC token: its workflow must grant permissions: id-token: write")
		}
		return refused("a fleet runs as a GitHub Actions job with permissions: id-token: write, and this is not one")
	}
	if err != nil {
		return refused("the job's Actions OIDC token could not be read (" + err.Error() + ")")
	}
	ans, err := in.Server.ActionsKey(tok, in.Engine)
	var ref *licenseapi.Refusal
	switch {
	case err == nil:
	case errors.As(err, &ref) && slices.Contains(transient, ref.Status):
		return Verdict{Transient: true, Notice: "[cn] fleet: the license server asked to retry later (" + ref.Error() + "); this run reaches nothing"}
	case errors.As(err, &ref) && ref.Status < 500:
		why := ref.Reason
		if why == "" {
			why = strconv.Itoa(ref.Status)
		}
		return refused("the license server refused the key (" + why + ")")
	default:
		return unverified("the license server did not answer: " + err.Error())
	}
	k, err := verify([]byte(ans.Key), in.Roots, in.Now)
	if err != nil {
		return refused("the key did not verify (" + err.Error() + ")")
	}
	if id, err := strconv.ParseInt(in.Getenv("GITHUB_REPOSITORY_ID"), 10, 64); err == nil && id != k.RepoID {
		return refused(fmt.Sprintf("the key names repo %d, not this job's %d", k.RepoID, id))
	}
	if !slices.Contains(FleetPlans, k.Plan) {
		return refused(fmt.Sprintf("%s is on the %s plan, which does not include a fleet", k.OwnerLogin, k.Plan))
	}
	return Verdict{Plan: k.Plan, OwnerID: k.OwnerID, OwnerLogin: k.OwnerLogin}
}

// key is what the check reads of a key's signed payload.
type key struct {
	Kid        string `json:"kid"`
	RepoID     int64  `json:"repo_id"`
	OwnerID    int64  `json:"owner_id"`
	OwnerLogin string `json:"owner_login"`
	Plan       string `json:"plan"`
	Exp        int64  `json:"exp"`
}

// verify checks a key's wire form: a license certificate that chains to
// roots at now, the payload's kid naming it, the signature over KeyDomain
// and the payload, and an expiry still ahead.
func verify(wire []byte, roots []ed25519.PublicKey, now time.Time) (key, error) {
	var env struct {
		Certificate *sign.Certificate `json:"certificate"`
		Payload     string            `json:"payload"`
		Signature   string            `json:"signature"`
	}
	if err := json.Unmarshal(wire, &env); err != nil || env.Certificate == nil {
		return key{}, errors.New("not a key")
	}
	payload, err1 := sign.DecodeB64(env.Payload)
	sig, err2 := sign.DecodeB64(env.Signature)
	var k key
	if err1 != nil || err2 != nil || json.Unmarshal(payload, &k) != nil {
		return key{}, errors.New("not a key")
	}
	body, err := env.Certificate.Verify(roots, sign.UseLicense, now)
	if err != nil {
		return key{}, err
	}
	subject, err := body.Subject()
	if err != nil {
		return key{}, err
	}
	if k.Kid != body.KeyID || !ed25519.Verify(subject, append([]byte(KeyDomain), payload...), sig) {
		return key{}, errors.New("bad signature")
	}
	if now.Unix() >= k.Exp {
		return key{}, errors.New("expired")
	}
	return k, nil
}
