package sign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func seedKey(b byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = b
	}
	return ed25519.NewKeyFromSeed(seed)
}

func pub(k ed25519.PrivateKey) ed25519.PublicKey { return k.Public().(ed25519.PublicKey) }

var (
	t0   = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	root = seedKey(1)
	alt  = seedKey(2)
	subj = seedKey(3)
)

func TestKeyID(t *testing.T) {
	p := pub(root)
	sum := sha256.Sum256(p)
	if got, want := KeyID(p), hex.EncodeToString(sum[:])[:16]; got != want {
		t.Fatalf("KeyID = %s, want %s", got, want)
	}
}

func mustIssue(t *testing.T, use Use, nb, na time.Time) Certificate {
	t.Helper()
	c, err := Issue(root, pub(subj), use, nb, na)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCertificateVerifiesOnlyAgainstItsRoot(t *testing.T) {
	c := mustIssue(t, UseManifest, t0, t0.AddDate(0, 0, 30))
	b, err := c.Verify([]ed25519.PublicKey{pub(root)}, UseManifest, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if b.Issuer != KeyID(pub(root)) || b.KeyID != KeyID(pub(subj)) {
		t.Fatalf("body %+v", b)
	}
	if _, err := c.Verify([]ed25519.PublicKey{pub(alt)}, UseManifest, t0.Add(time.Hour)); err == nil {
		t.Fatal("verified against another root")
	}
	if _, err := c.Verify([]ed25519.PublicKey{pub(alt), pub(root)}, UseManifest, t0.Add(time.Hour)); err != nil {
		t.Fatalf("a trust store holding the root must verify: %v", err)
	}
}

func TestUseMustMatch(t *testing.T) {
	m := mustIssue(t, UseManifest, t0, t0.AddDate(0, 0, 30))
	l := mustIssue(t, UseLicense, t0, t0.AddDate(0, 0, 30))
	roots := []ed25519.PublicKey{pub(root)}
	if _, err := m.Verify(roots, UseLicense, t0); err == nil {
		t.Error("manifest certificate accepted for license")
	}
	if _, err := l.Verify(roots, UseManifest, t0); err == nil {
		t.Error("license certificate accepted for manifest")
	}
}

func TestOnlyTheFourUsesIssue(t *testing.T) {
	for _, u := range []Use{UseManifest, UsePacks, UseLicense, UseLicensePublic} {
		if _, err := Issue(root, pub(subj), u, t0, t0.AddDate(0, 0, 30)); err != nil {
			t.Errorf("use %s refused: %v", u, err)
		}
	}
	for _, u := range []Use{"", "Manifest", "release", "license-private", "packs "} {
		if _, err := Issue(root, pub(subj), u, t0, t0.AddDate(0, 0, 30)); err == nil {
			t.Errorf("use %q issued", u)
		}
	}
}

func TestIssueCapsPerUse(t *testing.T) {
	if _, err := Issue(root, pub(subj), UseManifest, t0, t0.AddDate(0, 0, 365)); err != nil {
		t.Errorf("365-day manifest refused: %v", err)
	}
	if _, err := Issue(root, pub(subj), UseManifest, t0, t0.AddDate(0, 0, 366)); err == nil {
		t.Error("366-day manifest issued")
	}
	for _, u := range []Use{UsePacks, UseLicense, UseLicensePublic} {
		if _, err := Issue(root, pub(subj), u, t0, t0.AddDate(0, 0, 90)); err != nil {
			t.Errorf("90-day %s refused: %v", u, err)
		}
		if _, err := Issue(root, pub(subj), u, t0, t0.AddDate(0, 0, 91)); err == nil {
			t.Errorf("91-day %s issued", u)
		}
	}
	if _, err := Issue(root, pub(subj), UseManifest, t0, t0); err == nil {
		t.Error("empty validity issued")
	}
}

func TestValidityWindow(t *testing.T) {
	c := mustIssue(t, UsePacks, t0, t0.AddDate(0, 0, 90))
	roots := []ed25519.PublicKey{pub(root)}
	if _, err := c.Verify(roots, UsePacks, t0.Add(-time.Second)); err == nil {
		t.Error("not-yet-valid accepted")
	}
	if _, err := c.Verify(roots, UsePacks, t0.AddDate(0, 0, 90).Add(time.Second)); err == nil {
		t.Error("expired accepted")
	}
	if _, err := c.Verify(roots, UsePacks, t0.AddDate(0, 0, 45)); err != nil {
		t.Errorf("in-window refused: %v", err)
	}
}

func TestVerifyRefusesOverlongCertificate(t *testing.T) {
	c := issueUnchecked(root, pub(subj), UseManifest, t0, t0.AddDate(0, 0, 401))
	if _, err := c.Verify([]ed25519.PublicKey{pub(root)}, UseManifest, t0.AddDate(0, 0, 1)); err == nil {
		t.Fatal("a certificate valid for more than 400 days verified")
	}
	ok := issueUnchecked(root, pub(subj), UseManifest, t0, t0.AddDate(0, 0, 400))
	if _, err := ok.Verify([]ed25519.PublicKey{pub(root)}, UseManifest, t0.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("400 days exactly must verify: %v", err)
	}
}

func flip(s string, i int) string {
	b := []byte(s)
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b)
}

func TestSignedManifest(t *testing.T) {
	c := mustIssue(t, UseManifest, t0, t0.AddDate(0, 0, 30))
	roots := []ed25519.PublicKey{pub(root)}
	manifest := []byte(`{"v":1,"version":"1.1.0"}` + "\n")
	sig := SignManifest(subj, c, manifest)
	if _, err := VerifyManifest(sig, manifest, roots, t0); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte{}, manifest...)
	bad[3] ^= 1
	if _, err := VerifyManifest(sig, bad, roots, t0); err == nil {
		t.Error("flipped manifest byte verified")
	}
	s2 := sig
	s2.Signature = flip(sig.Signature, 5)
	if _, err := VerifyManifest(s2, manifest, roots, t0); err == nil {
		t.Error("flipped signature byte verified")
	}
	s3 := sig
	s3.Certificate.Payload = flip(sig.Certificate.Payload, 20)
	if _, err := VerifyManifest(s3, manifest, roots, t0); err == nil {
		t.Error("flipped certificate payload verified")
	}
	// A manifest signed by a key certified for another use is refused.
	lc := mustIssue(t, UseLicense, t0, t0.AddDate(0, 0, 30))
	if _, err := VerifyManifest(SignManifest(subj, lc, manifest), manifest, roots, t0); err == nil {
		t.Error("license-use key signed a manifest")
	}
	// A manifest signed by a key other than the certificate's subject is refused.
	if _, err := VerifyManifest(SignManifest(alt, c, manifest), manifest, roots, t0); err == nil {
		t.Error("signature by a non-subject key verified")
	}
}

func TestKeyFilesRoundTrip(t *testing.T) {
	priv, err := ParsePrivateKey(FormatPrivateKey(root))
	if err != nil || !priv.Equal(root) {
		t.Fatalf("private round trip: %v", err)
	}
	p, err := ParsePublicKey(FormatPublicKey(pub(root)))
	if err != nil || !p.Equal(pub(root)) {
		t.Fatalf("public round trip: %v", err)
	}
	if _, err := ParsePublicKey("not a key\n"); err == nil {
		t.Error("garbage parsed as a key")
	}
	if strings.Contains(FormatPublicKey(pub(root)), "=") {
		t.Error("key files are unpadded base64url")
	}
}

func TestSignedPackIndex(t *testing.T) {
	c := mustIssue(t, UsePacks, t0, t0.AddDate(0, 0, 30))
	roots := []ed25519.PublicKey{pub(root)}
	index := []byte(`{"v":1,"id":"hello","versions":[]}` + "\n")
	sig := SignPackIndex(subj, c, index)
	if b, err := VerifyPackIndex(sig, index, roots, t0); err != nil || b.Use != UsePacks {
		t.Fatalf("%v %+v", err, b)
	}
	bad := append([]byte{}, index...)
	bad[3] ^= 1
	if _, err := VerifyPackIndex(sig, bad, roots, t0); err == nil {
		t.Error("flipped index byte verified")
	}
	s2 := sig
	s2.Signature = flip(sig.Signature, 5)
	if _, err := VerifyPackIndex(s2, index, roots, t0); err == nil {
		t.Error("flipped signature byte verified")
	}
	mc := mustIssue(t, UseManifest, t0, t0.AddDate(0, 0, 30))
	if _, err := VerifyPackIndex(SignPackIndex(subj, mc, index), index, roots, t0); err == nil || !strings.Contains(err.Error(), "use") {
		t.Errorf("manifest-use certificate signed an index: %v", err)
	}
	// The same key and certificate, but the manifest domain: the domain
	// separates the two messages, so neither verifies as the other.
	if _, err := VerifyPackIndex(SignManifest(subj, c, index), index, roots, t0); err == nil {
		t.Error("an index signed under the manifest domain verified")
	}
	if _, err := VerifyManifest(SignPackIndex(subj, mc, index), index, roots, t0); err == nil {
		t.Error("a manifest signed under the pack index domain verified")
	}
}

// The reader of index.json decodes without DisallowUnknownFields, so Packs
// can add a field without an engine release; the signature covers the
// bytes as they are, whatever fields they hold.
func TestVerifyPackIndexAcceptsAnExtraField(t *testing.T) {
	c := mustIssue(t, UsePacks, t0, t0.AddDate(0, 0, 30))
	index := []byte(`{"v":1,"id":"hello","versions":[],"addedLater":{"x":1}}` + "\n")
	if _, err := VerifyPackIndex(SignPackIndex(subj, c, index), index, []ed25519.PublicKey{pub(root)}, t0); err != nil {
		t.Fatal(err)
	}
}

// A catalog is signed by a packs key under its own domain: neither an
// index's signature nor a catalog's verifies as the other's.
func TestSignedPackCatalog(t *testing.T) {
	c := mustIssue(t, UsePacks, t0, t0.AddDate(0, 0, 30))
	roots := []ed25519.PublicKey{pub(root)}
	cat := []byte(`{"v":1,"serial":3,"packs":[]}` + "\n")
	if b, err := VerifyPackCatalog(SignPackCatalog(subj, c, cat), cat, roots, t0); err != nil || b.Use != UsePacks {
		t.Fatalf("%v %+v", err, b)
	}
	if PackCatalogDomain != "claudinite-packcatalog-v1\n" {
		t.Errorf("the catalog domain is %q; ClaudinitePacks' tools/sign/sign.mjs signs under claudinite-packcatalog-v1", PackCatalogDomain)
	}
	if _, err := VerifyPackCatalog(SignPackIndex(subj, c, cat), cat, roots, t0); err == nil {
		t.Error("a catalog signed under the pack index domain verified")
	}
	if _, err := VerifyPackIndex(SignPackCatalog(subj, c, cat), cat, roots, t0); err == nil {
		t.Error("an index signed under the catalog domain verified")
	}
	mc := mustIssue(t, UseManifest, t0, t0.AddDate(0, 0, 30))
	if _, err := VerifyPackCatalog(SignPackCatalog(subj, mc, cat), cat, roots, t0); err == nil {
		t.Error("a manifest-use certificate signed a catalog")
	}
}
