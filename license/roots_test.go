package license

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

func TestEveryEmbeddedRootIsDistinct(t *testing.T) {
	roots, err := Roots()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) == 0 || len(roots)%2 != 0 {
		t.Fatalf("%d roots embedded, want a root and a standby per directory", len(roots))
	}
	seen := map[string]bool{}
	for _, r := range roots {
		id := sign.KeyID(r)
		if seen[id] {
			t.Fatalf("key id %s embedded twice", id)
		}
		seen[id] = true
	}
}
