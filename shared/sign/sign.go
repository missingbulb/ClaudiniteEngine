// Package sign holds the engine's Ed25519 signing chain: key ids, use-named
// certificates issued by a root key, and signed release manifests.
//
// Format (JWS-like, no canonicalization). A certificate is
//
//	{"payload": b64url(body JSON), "signature": b64url(Ed25519(CertDomain || body JSON))}
//
// where the body is {"v":1,"keyId","publicKey","use","issuer","notBefore",
// "notAfter"} and the signature covers the decoded body bytes. A signed
// manifest (manifest.sig.json) is
//
//	{"certificate": <cert>, "signature": b64url(Ed25519(ManifestDomain || SHA-512(manifest.json)))}
//
// A signed pack index (index.sig.json beside a pack's index.json on the
// ClaudinitePacks vendored branch) has the same shape, by a key certified
// for use packs:
//
//	{"certificate": <cert>, "signature": b64url(Ed25519(PackIndexDomain || SHA-512(index.json)))}
//
// b64url is base64url without padding. testdata/vectors.json pins all of it
// for the license Worker and ClaudinitePacks, which implement the same
// checks in JavaScript.
package sign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CertDomain     = "claudinite-cert-v1\n"
	ManifestDomain = "claudinite-manifest-v1\n"
	// PackIndexDomain covers a pack's index.json bytes as published, with
	// no canonical JSON. Its reader (phase 3) decodes index.json without
	// DisallowUnknownFields, ignoring unknown top-level and entry fields
	// and refusing only a "v" it does not know, so ClaudinitePacks can add
	// a field without an engine release; the certificate keeps its strict
	// decode.
	PackIndexDomain = "claudinite-packindex-v1\n"
)

// AbsoluteMaxValidity bounds any certificate at verify time, whatever its use.
const AbsoluteMaxValidity = 400 * 24 * time.Hour

var b64 = base64.RawURLEncoding

// Use names what a certified key may sign.
type Use string

const (
	UseManifest      Use = "manifest"
	UsePacks         Use = "packs"
	UseLicense       Use = "license"
	UseLicensePublic Use = "license-public"
)

// MaxValidity is the longest certificate a root issues for a use, and false
// for a string that is not one of the four uses.
func MaxValidity(u Use) (time.Duration, bool) {
	switch u {
	case UseManifest:
		return 365 * 24 * time.Hour, true
	case UsePacks, UseLicense, UseLicensePublic:
		return 90 * 24 * time.Hour, true
	}
	return 0, false
}

// KeyID is the first 16 hex characters of SHA-256 over the raw public key.
func KeyID(p ed25519.PublicKey) string {
	sum := sha256.Sum256(p)
	return hex.EncodeToString(sum[:])[:16]
}

// Body is a certificate's signed content.
type Body struct {
	V         int    `json:"v"`
	KeyID     string `json:"keyId"`
	PublicKey string `json:"publicKey"`
	Use       Use    `json:"use"`
	Issuer    string `json:"issuer"`
	NotBefore string `json:"notBefore"`
	NotAfter  string `json:"notAfter"`
}

// Certificate binds a subject key to a use, signed by a root.
type Certificate struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// Issue certifies subject for use, signed by root, refusing an unknown use
// and a validity longer than the use's cap.
func Issue(root ed25519.PrivateKey, subject ed25519.PublicKey, use Use, notBefore, notAfter time.Time) (Certificate, error) {
	maxV, ok := MaxValidity(use)
	if !ok {
		return Certificate{}, fmt.Errorf("unknown certificate use %q (want manifest, packs, license or license-public)", use)
	}
	if !notAfter.After(notBefore) {
		return Certificate{}, errors.New("notAfter must be after notBefore")
	}
	if notAfter.Sub(notBefore) > maxV {
		return Certificate{}, fmt.Errorf("use %s is capped at %d days", use, int(maxV.Hours()/24))
	}
	return issueUnchecked(root, subject, use, notBefore, notAfter), nil
}

func issueUnchecked(root ed25519.PrivateKey, subject ed25519.PublicKey, use Use, notBefore, notAfter time.Time) Certificate {
	body, _ := json.Marshal(Body{
		V:         1,
		KeyID:     KeyID(subject),
		PublicKey: b64.EncodeToString(subject),
		Use:       use,
		Issuer:    KeyID(root.Public().(ed25519.PublicKey)),
		NotBefore: notBefore.UTC().Format(time.RFC3339),
		NotAfter:  notAfter.UTC().Format(time.RFC3339),
	})
	return Certificate{
		Payload:   b64.EncodeToString(body),
		Signature: b64.EncodeToString(ed25519.Sign(root, append([]byte(CertDomain), body...))),
	}
}

// Errors a certificate check names, so a caller can report which check
// refused it. Any other error from Verify or VerifyAnyUse is a malformed
// certificate.
var (
	ErrUntrustedRoot   = errors.New("certificate is not signed by a trusted root")
	ErrCertUse         = errors.New("certificate use is not the one wanted")
	ErrCertKeyID       = errors.New("certificate keyId does not match its public key")
	ErrCertValidity    = errors.New("certificate validity exceeds 400 days")
	ErrCertNotYetValid = errors.New("certificate is not yet valid")
	ErrCertExpired     = errors.New("certificate has expired")
)

// Verify checks, in order: the certificate is signed by one of roots, its
// use is want, now is within its validity, and the validity is no longer
// than AbsoluteMaxValidity. It returns the body and the subject key.
func (c Certificate) Verify(roots []ed25519.PublicKey, want Use, now time.Time) (Body, error) {
	return c.verify(roots, &want, now)
}

// VerifyAnyUse is Verify accepting any of the four uses, for a caller that
// judges the use itself once the rest of what it verifies has passed.
func (c Certificate) VerifyAnyUse(roots []ed25519.PublicKey, now time.Time) (Body, error) {
	return c.verify(roots, nil, now)
}

func (c Certificate) verify(roots []ed25519.PublicKey, want *Use, now time.Time) (Body, error) {
	body, err := b64.DecodeString(c.Payload)
	if err != nil {
		return Body{}, fmt.Errorf("certificate payload: %w", err)
	}
	sig, err := b64.DecodeString(c.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Body{}, errors.New("certificate signature is malformed")
	}
	var b Body
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Body{}, fmt.Errorf("certificate body: %w", err)
	}
	var signer ed25519.PublicKey
	for _, r := range roots {
		if KeyID(r) == b.Issuer {
			signer = r
		}
	}
	if signer == nil || !ed25519.Verify(signer, append([]byte(CertDomain), body...), sig) {
		return Body{}, ErrUntrustedRoot
	}
	if b.V != 1 {
		return Body{}, fmt.Errorf("certificate version %d is not supported", b.V)
	}
	if _, ok := MaxValidity(b.Use); !ok {
		return Body{}, fmt.Errorf("certificate use %q is not one of the four", b.Use)
	}
	if want != nil && b.Use != *want {
		return Body{}, fmt.Errorf("%w: certificate use %q, want %q", ErrCertUse, b.Use, *want)
	}
	subject, err := b.Subject()
	if err != nil {
		return Body{}, err
	}
	if KeyID(subject) != b.KeyID {
		return Body{}, ErrCertKeyID
	}
	nb, err1 := time.Parse(time.RFC3339, b.NotBefore)
	na, err2 := time.Parse(time.RFC3339, b.NotAfter)
	if err1 != nil || err2 != nil {
		return Body{}, errors.New("certificate validity times are malformed")
	}
	if !na.After(nb) || na.Sub(nb) > AbsoluteMaxValidity {
		return Body{}, ErrCertValidity
	}
	if now.Before(nb) {
		return Body{}, ErrCertNotYetValid
	}
	if now.After(na) {
		return Body{}, ErrCertExpired
	}
	return b, nil
}

// Subject decodes the certified public key.
func (b Body) Subject() (ed25519.PublicKey, error) {
	p, err := b64.DecodeString(b.PublicKey)
	if err != nil || len(p) != ed25519.PublicKeySize {
		return nil, errors.New("certificate public key is malformed")
	}
	return ed25519.PublicKey(p), nil
}

// Detached is a signature that travels beside the bytes it covers, with
// the certificate of the key that made it: manifest.sig.json and a pack's
// index.sig.json.
type Detached struct {
	Certificate Certificate `json:"certificate"`
	Signature   string      `json:"signature"`
}

// SignedManifest is the content of manifest.sig.json.
type SignedManifest = Detached

// SignedPackIndex is the content of a pack's index.sig.json.
type SignedPackIndex = Detached

func message(domain string, data []byte) []byte {
	sum := sha512.Sum512(data)
	return append([]byte(domain), sum[:]...)
}

func signDetached(domain string, key ed25519.PrivateKey, cert Certificate, data []byte) Detached {
	return Detached{Certificate: cert, Signature: b64.EncodeToString(ed25519.Sign(key, message(domain, data)))}
}

func verifyDetached(domain string, use Use, what string, s Detached, data []byte, roots []ed25519.PublicKey, now time.Time) (Body, error) {
	b, err := s.Certificate.Verify(roots, use, now)
	if err != nil {
		return Body{}, err
	}
	subject, err := b.Subject()
	if err != nil {
		return Body{}, err
	}
	sig, err := b64.DecodeString(s.Signature)
	if err != nil || !ed25519.Verify(subject, message(domain, data), sig) {
		return Body{}, errors.New(what + " signature does not verify")
	}
	return b, nil
}

// SignManifest signs manifest.json's bytes with key, attaching cert.
func SignManifest(key ed25519.PrivateKey, cert Certificate, manifest []byte) SignedManifest {
	return signDetached(ManifestDomain, key, cert, manifest)
}

// VerifyManifest checks the certificate for use manifest against roots, then
// the signature over manifest by the certificate's subject.
func VerifyManifest(s SignedManifest, manifest []byte, roots []ed25519.PublicKey, now time.Time) (Body, error) {
	return verifyDetached(ManifestDomain, UseManifest, "manifest", s, manifest, roots, now)
}

// SignPackIndex signs a pack's index.json bytes with key, attaching cert.
func SignPackIndex(key ed25519.PrivateKey, cert Certificate, index []byte) SignedPackIndex {
	return signDetached(PackIndexDomain, key, cert, index)
}

// VerifyPackIndex checks the certificate for use packs against roots, then
// the signature over index by the certificate's subject.
func VerifyPackIndex(s SignedPackIndex, index []byte, roots []ed25519.PublicKey, now time.Time) (Body, error) {
	return verifyDetached(PackIndexDomain, UsePacks, "pack index", s, index, roots, now)
}

// FormatPrivateKey renders a key file: the base64url seed and a newline.
func FormatPrivateKey(k ed25519.PrivateKey) string { return b64.EncodeToString(k.Seed()) + "\n" }

// FormatPublicKey renders a public key file: base64url and a newline.
func FormatPublicKey(p ed25519.PublicKey) string { return b64.EncodeToString(p) + "\n" }

// ParsePrivateKey reads a key file written by FormatPrivateKey.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	seed, err := b64.DecodeString(strings.TrimSpace(s))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("not an Ed25519 private key file")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// ParsePublicKey reads a key file written by FormatPublicKey.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	p, err := b64.DecodeString(strings.TrimSpace(s))
	if err != nil || len(p) != ed25519.PublicKeySize {
		return nil, errors.New("not an Ed25519 public key file")
	}
	return ed25519.PublicKey(p), nil
}

// DecodeB64 decodes the unpadded base64url this package writes.
func DecodeB64(s string) ([]byte, error) { return b64.DecodeString(s) }
