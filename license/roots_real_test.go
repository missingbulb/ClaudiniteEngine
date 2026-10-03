//go:build !devroots

package license

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// A build without the devroots tag, every released cn, trusts exactly the
// root and standby root the key ceremony (#5) made.
func TestReleasedBuildEmbedsTheCeremonyRoots(t *testing.T) {
	roots, err := Roots()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ea85f35421f375fc", "196c6acb9cc31774"}
	if len(roots) != len(want) {
		t.Fatalf("%d roots embedded, want %d", len(roots), len(want))
	}
	for i, r := range roots {
		if got := sign.KeyID(r); got != want[i] {
			t.Errorf("embedded root %d has key id %s, want %s", i, got, want[i])
		}
	}
}
