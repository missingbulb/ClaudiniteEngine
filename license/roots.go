package license

import (
	"crypto/ed25519"
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// Roots returns the embedded root and standby root public keys, in that order.
func Roots() ([]ed25519.PublicKey, error) {
	var out []ed25519.PublicKey
	for _, name := range []string{"root.pub", "standby.pub"} {
		raw, err := rootFiles.ReadFile(rootDir + "/" + name)
		if err != nil {
			return nil, err
		}
		p, err := sign.ParsePublicKey(string(raw))
		if err != nil {
			return nil, fmt.Errorf("embedded %s/%s: %w", rootDir, name, err)
		}
		out = append(out, p)
	}
	return out, nil
}
