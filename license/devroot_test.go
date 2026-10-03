package license

import (
	"bytes"
	"os"
	"testing"
)

// A stable release must never embed the development roots (devroots/).
// The promotion job builds with -tags stable.
func TestStableBuildDoesNotEmbedTheDevRoot(t *testing.T) {
	if !stableBuild {
		t.Skip("not a stable build")
	}
	for _, name := range []string{"root", "standby"} {
		embedded, err := rootFiles.ReadFile(rootDir + "/" + name + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		dev, err := os.ReadFile("devroots/" + name + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(bytes.TrimSpace(embedded), bytes.TrimSpace(dev)) {
			t.Errorf("the build embeds the development %s from license/devroots/", name)
		}
	}
}
