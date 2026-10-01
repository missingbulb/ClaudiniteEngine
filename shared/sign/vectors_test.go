package sign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"os"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/vectors.json")

const vectorsPath = "testdata/vectors.json"

// The generator is deterministic (fixed seeds, fixed times, Ed25519), so the
// committed file must equal a fresh generation.
func TestVectorsFileIsCurrent(t *testing.T) {
	got, err := generateVectors()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(vectorsPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("testdata/vectors.json is stale; run go test ./shared/sign -run TestVectorsFileIsCurrent -update")
	}
}

// Replays the file the way the license Worker will: only what the file says.
func TestVectorsReplay(t *testing.T) {
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.CertificateCases) == 0 || len(v.MessageCases) == 0 {
		t.Fatal("vectors carry no cases")
	}
	if KeyID(mustB64(t, v.KeyID.PublicKey)) != v.KeyID.KeyID {
		t.Error("key id vector mismatch")
	}
	rootPub := func(name string) ed25519.PublicKey { return mustB64(t, v.Roots[name].PublicKey) }
	for _, c := range v.CertificateCases {
		now, _ := time.Parse(time.RFC3339, c.Now)
		_, err := c.Certificate.Verify([]ed25519.PublicKey{rootPub(c.Root)}, Use(c.Use), now)
		if (err == nil) != c.Valid {
			t.Errorf("certificate case %q: err=%v, want valid=%v", c.Name, err, c.Valid)
		}
	}
	for _, c := range v.MessageCases {
		now, _ := time.Parse(time.RFC3339, c.Now)
		msg := mustB64(t, c.Message)
		_, err := VerifyManifest(c.Signed, msg, []ed25519.PublicKey{rootPub(c.Root)}, now)
		if (err == nil) != c.Valid {
			t.Errorf("message case %q: err=%v, want valid=%v", c.Name, err, c.Valid)
		}
	}
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
