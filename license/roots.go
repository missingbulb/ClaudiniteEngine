package license

import (
	"crypto/ed25519"
	"embed"
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

//go:embed roots/root.pub roots/standby.pub
var rootFiles embed.FS

// Roots returns the embedded root and standby root public keys, in that order.
func Roots() ([]ed25519.PublicKey, error) {
	var out []ed25519.PublicKey
	for _, name := range []string{"roots/root.pub", "roots/standby.pub"} {
		raw, err := rootFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		p, err := sign.ParsePublicKey(string(raw))
		if err != nil {
			return nil, fmt.Errorf("embedded %s: %w", name, err)
		}
		out = append(out, p)
	}
	return out, nil
}
