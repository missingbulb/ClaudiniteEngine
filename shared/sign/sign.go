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
// b64url is base64url without padding. testdata/vectors.json pins all of it
// for the license Worker, which implements the same checks in JavaScript.
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

// Verify checks, in order: the certificate is signed by one of roots, its
// use is want, now is within its validity, and the validity is no longer
// than AbsoluteMaxValidity. It returns the body and the subject key.
func (c Certificate) Verify(roots []ed25519.PublicKey, want Use, now time.Time) (Body, error) {
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
		return Body{}, errors.New("certificate is not signed by a trusted root")
	}
	if b.V != 1 {
		return Body{}, fmt.Errorf("certificate version %d is not supported", b.V)
	}
	if _, ok := MaxValidity(b.Use); !ok || b.Use != want {
		return Body{}, fmt.Errorf("certificate use %q, want %q", b.Use, want)
	}
	subject, err := b.Subject()
	if err != nil {
		return Body{}, err
	}
	if KeyID(subject) != b.KeyID {
		return Body{}, errors.New("certificate keyId does not match its public key")
	}
	nb, err1 := time.Parse(time.RFC3339, b.NotBefore)
	na, err2 := time.Parse(time.RFC3339, b.NotAfter)
	if err1 != nil || err2 != nil {
		return Body{}, errors.New("certificate validity times are malformed")
	}
	if !na.After(nb) || na.Sub(nb) > AbsoluteMaxValidity {
		return Body{}, errors.New("certificate validity exceeds 400 days")
	}
	if now.Before(nb) {
		return Body{}, errors.New("certificate is not yet valid")
	}
	if now.After(na) {
		return Body{}, errors.New("certificate has expired")
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

// SignedManifest is the content of manifest.sig.json.
type SignedManifest struct {
	Certificate Certificate `json:"certificate"`
	Signature   string      `json:"signature"`
}

func manifestMessage(manifest []byte) []byte {
	sum := sha512.Sum512(manifest)
	return append([]byte(ManifestDomain), sum[:]...)
}

// SignManifest signs manifest.json's bytes with key, attaching cert.
func SignManifest(key ed25519.PrivateKey, cert Certificate, manifest []byte) SignedManifest {
	return SignedManifest{
		Certificate: cert,
		Signature:   b64.EncodeToString(ed25519.Sign(key, manifestMessage(manifest))),
	}
}

// VerifyManifest checks the certificate for use manifest against roots, then
// the signature over manifest by the certificate's subject.
func VerifyManifest(s SignedManifest, manifest []byte, roots []ed25519.PublicKey, now time.Time) (Body, error) {
	b, err := s.Certificate.Verify(roots, UseManifest, now)
	if err != nil {
		return Body{}, err
	}
	subject, err := b.Subject()
	if err != nil {
		return Body{}, err
	}
	sig, err := b64.DecodeString(s.Signature)
	if err != nil || !ed25519.Verify(subject, manifestMessage(manifest), sig) {
		return Body{}, errors.New("manifest signature does not verify")
	}
	return b, nil
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
