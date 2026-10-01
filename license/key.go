package license

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// KeyDomain prefixes the payload bytes a license key's signature covers.
const KeyDomain = "claudinite-license-v1\n"

// IatLeeway is how far ahead of the verifier's clock a key's iat may be.
const IatLeeway = 300 * time.Second

// Plan is the plan a key was issued for.
type Plan string

const (
	PlanPublic       Plan = "public"
	PlanPrivateRepo  Plan = "private-repo"
	PlanPersonal     Plan = "personal"
	PlanOrganization Plan = "organization"
	PlanInternal     Plan = "internal"
)

// Plans is every plan a key may carry.
var Plans = []Plan{PlanPublic, PlanPrivateRepo, PlanPersonal, PlanOrganization, PlanInternal}

var (
	keyTypes = []string{"session", "actions", "grant"}
	states   = []string{"ok", "grace", "degraded", "unverified"}
	keyIDRe  = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// Release is what a key says about engine releases and pack-index keys.
type Release struct {
	Held            []string `json:"held"`
	Revoked         []string `json:"revoked"`
	SecurityFixes   []string `json:"security_fixes"`
	PackIndexSerial int64    `json:"pack_index_serial"`
	PackKeys        []string `json:"pack_keys"`
}

// KeyPayload is a license key's signed content. A field it does not name is
// ignored, so the issuer may add one without an engine release.
type KeyPayload struct {
	V          int      `json:"v"`
	Typ        string   `json:"typ"`
	Kid        string   `json:"kid"`
	RepoID     int64    `json:"repo_id"`
	OwnerID    int64    `json:"owner_id"`
	OwnerType  string   `json:"owner_type"`
	OwnerLogin string   `json:"owner_login"`
	Plan       Plan     `json:"plan"`
	UserID     *int64   `json:"user_id,omitempty"`
	Nonce      string   `json:"nonce,omitempty"`
	Iat        int64    `json:"iat"`
	Exp        int64    `json:"exp"`
	State      string   `json:"state"`
	GraceUntil *int64   `json:"grace_until"`
	Features   []string `json:"features"`
	Release    Release  `json:"release"`
}

// Reason names the check that refused a key, in ClaudiniteLicenses'
// vocabulary.
type Reason string

const (
	ReasonShape           Reason = "shape"
	ReasonUntrustedRoot   Reason = "untrusted-root"
	ReasonCertKeyID       Reason = "cert-key-id"
	ReasonCertValidity    Reason = "cert-validity"
	ReasonCertNotYetValid Reason = "cert-not-yet-valid"
	ReasonCertExpired     Reason = "cert-expired"
	ReasonKidMismatch     Reason = "kid-mismatch"
	ReasonBadSignature    Reason = "bad-signature"
	ReasonKeyNotYetValid  Reason = "key-not-yet-valid"
	ReasonKeyExpired      Reason = "key-expired"
	ReasonPurpose         Reason = "purpose"
)

// KeyError is a refused key and the check that refused it.
type KeyError struct {
	Reason Reason
	Err    error
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("license key refused (%s): %v", e.Reason, e.Err)
}
func (e *KeyError) Unwrap() error { return e.Err }

// ReasonOf is the reason a VerifyKey error names, and "" for any other error.
func ReasonOf(err error) Reason {
	var k *KeyError
	if errors.As(err, &k) {
		return k.Reason
	}
	return ""
}

func refuse(r Reason, format string, a ...any) error {
	return &KeyError{r, fmt.Errorf(format, a...)}
}

// Signs reports whether a certificate of use may sign a key for plan: a
// license certificate signs any plan, a license-public certificate only the
// public plan, and a packs or manifest certificate never signs a key.
func Signs(use sign.Use, plan Plan) bool {
	switch use {
	case sign.UseLicense:
		return true
	case sign.UseLicensePublic:
		return plan == PlanPublic
	}
	return false
}

// VerifyKey checks a key's wire form in order: shape, the certificate
// against roots at now, kid against the certificate, the key signature, the
// key's iat (less IatLeeway) and exp, and the purpose (Signs). The first
// check that fails names the KeyError's reason.
func VerifyKey(key []byte, roots []ed25519.PublicKey, now time.Time) (KeyPayload, error) {
	var env struct {
		Certificate *sign.Certificate `json:"certificate"`
		Payload     *string           `json:"payload"`
		Signature   *string           `json:"signature"`
	}
	if err := json.Unmarshal(key, &env); err != nil || env.Certificate == nil || env.Payload == nil || env.Signature == nil {
		return KeyPayload{}, refuse(ReasonShape, "the envelope is not {certificate, payload, signature}")
	}
	payload, err := sign.DecodeB64(*env.Payload)
	if err != nil {
		return KeyPayload{}, refuse(ReasonShape, "payload: %v", err)
	}
	p, err := decodePayload(payload)
	if err != nil {
		return KeyPayload{}, refuse(ReasonShape, "payload: %v", err)
	}
	sig, err := sign.DecodeB64(*env.Signature)
	if err != nil {
		return KeyPayload{}, refuse(ReasonShape, "signature: %v", err)
	}
	body, err := env.Certificate.VerifyAnyUse(roots, now)
	if err != nil {
		return KeyPayload{}, &KeyError{certReason(err), err}
	}
	if p.Kid != body.KeyID {
		return KeyPayload{}, refuse(ReasonKidMismatch, "kid %q, certificate key id %q", p.Kid, body.KeyID)
	}
	subject, err := body.Subject()
	if err != nil {
		return KeyPayload{}, refuse(ReasonShape, "%v", err)
	}
	if !ed25519.Verify(subject, append([]byte(KeyDomain), payload...), sig) {
		return KeyPayload{}, refuse(ReasonBadSignature, "the key signature does not verify with the certified key")
	}
	nowS := float64(now.UnixMilli()) / 1000
	if nowS < float64(p.Iat)-IatLeeway.Seconds() {
		return KeyPayload{}, refuse(ReasonKeyNotYetValid, "issued at %d, more than %v ahead", p.Iat, IatLeeway)
	}
	if nowS >= float64(p.Exp) {
		return KeyPayload{}, refuse(ReasonKeyExpired, "expired at %d", p.Exp)
	}
	if !Signs(body.Use, p.Plan) {
		return KeyPayload{}, refuse(ReasonPurpose, "a %s certificate does not sign a %s plan key", body.Use, p.Plan)
	}
	return p, nil
}

func certReason(err error) Reason {
	for _, m := range []struct {
		err error
		r   Reason
	}{
		{sign.ErrUntrustedRoot, ReasonUntrustedRoot},
		{sign.ErrCertKeyID, ReasonCertKeyID},
		{sign.ErrCertValidity, ReasonCertValidity},
		{sign.ErrCertNotYetValid, ReasonCertNotYetValid},
		{sign.ErrCertExpired, ReasonCertExpired},
	} {
		if errors.Is(err, m.err) {
			return m.r
		}
	}
	return ReasonShape
}

// decodePayload refuses a payload missing a field the checks read, or
// holding one outside its closed set; anything it does not name passes.
func decodePayload(raw []byte) (KeyPayload, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return KeyPayload{}, err
	}
	for _, f := range []string{"v", "typ", "kid", "plan", "iat", "exp", "state", "features", "release"} {
		if v, ok := fields[f]; !ok || bytes.Equal(v, []byte("null")) {
			return KeyPayload{}, fmt.Errorf("%s is missing", f)
		}
	}
	var release map[string]json.RawMessage
	if err := json.Unmarshal(fields["release"], &release); err != nil {
		return KeyPayload{}, fmt.Errorf("release: %v", err)
	}
	for _, f := range []string{"held", "revoked", "security_fixes", "pack_index_serial", "pack_keys"} {
		if v, ok := release[f]; !ok || bytes.Equal(v, []byte("null")) {
			return KeyPayload{}, fmt.Errorf("release.%s is missing", f)
		}
	}
	var p KeyPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return KeyPayload{}, err
	}
	switch {
	case p.V != 1:
		return KeyPayload{}, fmt.Errorf("version %d is not supported", p.V)
	case !slices.Contains(keyTypes, p.Typ):
		return KeyPayload{}, fmt.Errorf("typ %q", p.Typ)
	case !slices.Contains(Plans, p.Plan):
		return KeyPayload{}, fmt.Errorf("plan %q", p.Plan)
	case !slices.Contains(states, p.State):
		return KeyPayload{}, fmt.Errorf("state %q", p.State)
	case !nonEmpty(p.Features) || len(slices.Compact(slices.Sorted(slices.Values(p.Features)))) != len(p.Features):
		return KeyPayload{}, errors.New("features are not distinct non-empty names")
	case !nonEmpty(p.Release.Held) || !nonEmpty(p.Release.Revoked) || !nonEmpty(p.Release.SecurityFixes):
		return KeyPayload{}, errors.New("release holds an empty version")
	case p.Release.PackIndexSerial < 0 || p.Release.PackIndexSerial > 1<<53-1:
		return KeyPayload{}, fmt.Errorf("release.pack_index_serial %d", p.Release.PackIndexSerial)
	}
	for _, k := range p.Release.PackKeys {
		if !keyIDRe.MatchString(k) {
			return KeyPayload{}, fmt.Errorf("release.pack_keys entry %q is not a key id", k)
		}
	}
	return p, nil
}

func nonEmpty(list []string) bool {
	return !slices.Contains(list, "")
}
