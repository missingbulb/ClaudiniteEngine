package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// testdata/keys.json is ClaudiniteLicenses' packages/signing/vectors/keys.json,
// copied byte for byte: the Worker that issues keys and this verifier must
// agree on every case.
type keyVectors struct {
	Domains map[string]string `json:"domains"`
	Now     string            `json:"now"`
	Roots   map[string]struct {
		PublicKey string `json:"publicKey"`
	} `json:"roots"`
	KeyCases []struct {
		Name   string `json:"name"`
		Key    string `json:"key"`
		Valid  bool   `json:"valid"`
		Reason Reason `json:"reason"`
	} `json:"keyCases"`
}

func TestKeyVectorsReplay(t *testing.T) {
	raw, err := os.ReadFile("testdata/keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var v keyVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.KeyCases) == 0 {
		t.Fatal("vectors carry no key cases")
	}
	if v.Domains["license"] != KeyDomain || v.Domains["certificate"] != sign.CertDomain {
		t.Fatalf("domains %q, want license %q and certificate %q", v.Domains, KeyDomain, sign.CertDomain)
	}
	now, err := time.Parse(time.RFC3339, v.Now)
	if err != nil {
		t.Fatal(err)
	}
	var roots []ed25519.PublicKey
	for _, name := range []string{"root", "standby"} {
		p, err := sign.ParsePublicKey(v.Roots[name].PublicKey)
		if err != nil {
			t.Fatalf("root %s: %v", name, err)
		}
		roots = append(roots, p)
	}
	for _, c := range v.KeyCases {
		t.Run(c.Name, func(t *testing.T) {
			_, err := VerifyKey([]byte(c.Key), roots, now)
			if c.Valid {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if got := ReasonOf(err); got != c.Reason {
				t.Fatalf("reason %q (%v), want %q", got, err, c.Reason)
			}
		})
	}
}

func seeded(b byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = b
	}
	return ed25519.NewKeyFromSeed(seed)
}

// signedKey issues a certificate of use from root and signs a minimal key
// for plan with its subject.
func signedKey(t *testing.T, root ed25519.PrivateKey, use sign.Use, plan Plan, now time.Time) []byte {
	t.Helper()
	issuing := seeded(9)
	cert, err := sign.Issue(root, issuing.Public().(ed25519.PublicKey), use, now.Add(-time.Hour), now.AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"v": 1, "typ": "session", "kid": sign.KeyID(issuing.Public().(ed25519.PublicKey)),
		"repo_id": 1, "owner_id": 2, "owner_type": "User", "owner_login": "acme-user",
		"plan": plan, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"state": "ok", "grace_until": nil, "features": []string{},
		"release": map[string]any{"held": []string{}, "revoked": []string{}, "security_fixes": []string{}, "pack_index_serial": 0, "pack_keys": []string{}},
	})
	b64 := base64.RawURLEncoding
	key, _ := json.Marshal(map[string]any{
		"certificate": cert,
		"payload":     b64.EncodeToString(payload),
		"signature":   b64.EncodeToString(ed25519.Sign(issuing, append([]byte(KeyDomain), payload...))),
	})
	return key
}

func TestPurposeRule(t *testing.T) {
	root := seeded(1)
	roots := []ed25519.PublicKey{root.Public().(ed25519.PublicKey)}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, use := range []sign.Use{sign.UseLicense, sign.UseLicensePublic, sign.UsePacks, sign.UseManifest} {
		for _, plan := range Plans {
			signs := use == sign.UseLicense || (use == sign.UseLicensePublic && plan == PlanPublic)
			_, err := VerifyKey(signedKey(t, root, use, plan, now), roots, now)
			switch {
			case signs && err != nil:
				t.Errorf("%s certificate on a %s key refused: %v", use, plan, err)
			case !signs && ReasonOf(err) != ReasonPurpose:
				t.Errorf("%s certificate on a %s key: %v, want reason purpose", use, plan, err)
			}
		}
	}
}

func TestReasonOfAForeignError(t *testing.T) {
	if got := ReasonOf(errors.New("other")); got != "" {
		t.Fatalf("ReasonOf(other) = %q", got)
	}
	if got := ReasonOf(nil); got != "" {
		t.Fatalf("ReasonOf(nil) = %q", got)
	}
}
