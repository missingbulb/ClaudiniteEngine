package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// testNow is the clock the license tests verify at.
var testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// testRoot certifies every key mint signs.
var testRoot = seeded(1)

func testRoots() []ed25519.PublicKey {
	return []ed25519.PublicKey{testRoot.Public().(ed25519.PublicKey)}
}

// basePayload is a valid public-plan session key for repo 11 owned by
// User 3, user 7 and nonce "nonce-0123456789ab", issued at testNow.
func basePayload() map[string]any {
	return map[string]any{
		"v": 1, "typ": "session", "repo_id": 11, "owner_id": 3, "owner_type": "User", "owner_login": "acme",
		"plan": "public", "user_id": 7, "nonce": "nonce-0123456789ab", "iat": testNow.Unix(), "exp": testNow.Add(7 * 24 * time.Hour).Unix(),
		"state": "ok", "grace_until": nil, "features": []string{"work-checks", "forced-skill-loading", "in-session-growth", "claudinite-tasks", "updates"},
		"release": map[string]any{"held": []string{}, "revoked": []string{}, "security_fixes": []string{}, "pack_index_serial": 0, "pack_keys": []string{}},
	}
}

// mint signs basePayload with edits applied (a nil value deletes the
// field) under a license certificate from testRoot.
func mint(t *testing.T, edits map[string]any) []byte {
	t.Helper()
	p := basePayload()
	for k, v := range edits {
		if v == nil {
			delete(p, k)
			continue
		}
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
	key, _ := json.Marshal(map[string]any{
		"certificate": cert,
		"payload":     b64.EncodeToString(payload),
		"signature":   b64.EncodeToString(ed25519.Sign(issuing, append([]byte(KeyDomain), payload...))),
	})
	return key
}

// minted verifies a minted key at testNow.
func minted(t *testing.T, edits map[string]any) KeyPayload {
	t.Helper()
	k, err := VerifyKey(mint(t, edits), testRoots(), testNow)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
