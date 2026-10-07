//go:build !devroots

package trust

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/sign"
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

// dev/release/create/manifest and dev/release/publish/promote verify against every *.pub in
// cn/shared/trust/roots/, so the directory holds the two ceremony roots and nothing
// else.
func TestTheRootsDirectoryHoldsOnlyTheTwoRoots(t *testing.T) {
	entries, err := os.ReadDir("roots")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if got := strings.Join(names, " "); got != "root.pub standby.pub" {
		t.Fatalf("cn/shared/trust/roots/ holds %s, want root.pub standby.pub", got)
	}
}
