package license

import (
	"bytes"
	"os"
	"testing"
)

// A stable release must never embed the development root (keys/dev/).
// The promotion job builds with -tags stable; until #5 replaces the roots,
// that build fails here on purpose.
func TestStableBuildDoesNotEmbedTheDevRoot(t *testing.T) {
	if !stableBuild {
		t.Skip("not a stable build; #5 (root key ceremony) replaces the dev roots before any stable release")
	}
	for _, name := range []string{"root", "standby"} {
		embedded, err := rootFiles.ReadFile("roots/" + name + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		dev, err := os.ReadFile("../keys/dev/" + name + ".pub")
		if err != nil {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(embedded), bytes.TrimSpace(dev)) {
			t.Errorf("license/roots/%s.pub is the development key from keys/dev/", name)
		}
	}
}
