package adopt

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
)

func TestCheckOwnManifest(t *testing.T) {
	roots := []ed25519.PublicKey{root.Public().(ed25519.PublicKey)}
	dir := t.TempDir()
	exe := filepath.Join(dir, "cn")
	_ = os.WriteFile(exe, []byte("the binary"), 0o555)
	if err := CheckOwnManifest(exe, roots, "linux-x64", now); err != nil {
		t.Errorf("a cn outside the cache: %v", err)
	}
	sum := sha256.Sum256([]byte("the binary"))
	manifest := []byte(`{"binaries": {"linux-x64": {"file": "cn", "sha256": "` + hex.EncodeToString(sum[:]) + `"}}}`)
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0o444)
	if err := CheckOwnManifest(exe, roots, "linux-x64", now); err == nil || !strings.Contains(err.Error(), "unsigned") {
		t.Errorf("unsigned: %v", err)
	}
	cert, _ := sign.Issue(root, relKey.Public().(ed25519.PublicKey), sign.UseManifest, now.AddDate(0, 0, -1), now.AddDate(0, 0, 30))
	sig, _ := json.Marshal(sign.SignManifest(relKey, cert, manifest))
	_ = os.WriteFile(filepath.Join(dir, "manifest.sig.json"), sig, 0o444)
	if err := CheckOwnManifest(exe, roots, "linux-x64", now); err != nil {
		t.Errorf("signed: %v", err)
	}
	if err := CheckOwnManifest(exe, roots, "darwin-arm64", now); err == nil || !strings.Contains(err.Error(), "binary hash") {
		t.Errorf("another platform: %v", err)
	}
	other := ed25519.NewKeyFromSeed(make([]byte, 32))
	if err := CheckOwnManifest(exe, []ed25519.PublicKey{other.Public().(ed25519.PublicKey)}, "linux-x64", now); err == nil || !strings.Contains(err.Error(), "manifest signature") {
		t.Errorf("another root: %v", err)
	}
}
