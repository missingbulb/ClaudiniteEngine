package adopt

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// CheckOwnManifest refuses to adopt from a cn the launcher's bootstrap
// placed beside a manifest.json that is not signed by the roots, or whose
// entry for this platform does not name this binary. A cn not run from a
// cache folder (no manifest.json beside it, a development build) passes.
func CheckOwnManifest(exe string, roots []ed25519.PublicKey, platform string, now time.Time) error {
	dir := filepath.Dir(exe)
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	rawSig, err := os.ReadFile(filepath.Join(dir, "manifest.sig.json"))
	if err != nil {
		return fmt.Errorf("manifest signature: %s holds no readable manifest.sig.json, and cn init never runs from an unsigned manifest", dir)
	}
	var s sign.SignedManifest
	if err := json.Unmarshal(rawSig, &s); err != nil {
		return fmt.Errorf("manifest signature: manifest.sig.json: %w", err)
	}
	if _, err := sign.VerifyManifest(s, manifest, roots, now); err != nil {
		return fmt.Errorf("manifest signature: %s: %w", dir, err)
	}
	var m struct {
		Binaries map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"binaries"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return fmt.Errorf("manifest.json: %w", err)
	}
	bin, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(bin)
	if e, ok := m.Binaries[platform]; !ok || e.SHA256 != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("binary hash: %s is not the %s binary its signed manifest names", exe, platform)
	}
	return nil
}
