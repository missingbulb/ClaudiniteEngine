//go:build devroots

package trust

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// A devroots build trusts the ceremony's roots and, after them, the
// development roots.
func TestDevrootsBuildAddsTheDevelopmentRoots(t *testing.T) {
	roots, err := Roots()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range roots {
		got = append(got, sign.KeyID(r))
	}
	want := []string{"ea85f35421f375fc", "196c6acb9cc31774", "4e445e16c1bb8d61", "0d8e65ad8093ff2e"}
	if len(got) != len(want) {
		t.Fatalf("key ids %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key ids %v, want %v", got, want)
		}
	}
}
