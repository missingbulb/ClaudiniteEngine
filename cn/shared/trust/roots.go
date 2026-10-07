package trust

import (
	"crypto/ed25519"
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/sign"
)

// Roots returns the embedded roots, each directory's root then its standby
// root.
func Roots() ([]ed25519.PublicKey, error) {
	var out []ed25519.PublicKey
	for _, dir := range rootDirs {
		for _, name := range []string{"root.pub", "standby.pub"} {
			raw, err := rootFiles.ReadFile(dir + "/" + name)
			if err != nil {
				return nil, err
			}
			p, err := sign.ParsePublicKey(string(raw))
			if err != nil {
				return nil, fmt.Errorf("embedded %s/%s: %w", dir, name, err)
			}
			out = append(out, p)
		}
	}
	return out, nil
}
