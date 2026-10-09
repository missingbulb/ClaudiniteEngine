package trust

import (
	"bytes"
	"os"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
)

// A stable release must never embed the development roots (devroots/).
// The promotion job builds with -tags stable.
func TestStableBuildDoesNotEmbedTheDevRoot(t *testing.T) {
	if !stableBuild {
		t.Skip("not a stable build")
	}
	roots, err := Roots()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root", "standby"} {
		raw, err := os.ReadFile("devroots/" + name + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		dev, err := sign.ParsePublicKey(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range roots {
			if bytes.Equal(r, dev) {
				t.Errorf("the build embeds the development %s from cn/packaging/trust/devroots/", name)
			}
		}
	}
}
