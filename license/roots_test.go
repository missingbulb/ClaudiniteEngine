package license

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

func TestExactlyTwoDistinctRootsEmbed(t *testing.T) {
	roots, err := Roots()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("%d roots embedded, want 2", len(roots))
	}
	if sign.KeyID(roots[0]) == sign.KeyID(roots[1]) {
		t.Fatal("root and standby share a key id")
	}
}
