package promotescope

import (
	"strings"
	"testing"
)

func TestRootsKeepTheShelfFirstAndNormalizeEachPath(t *testing.T) {
	got := Roots(map[string]any{"write_paths": []any{" skills ", "./prompts/", "packs", "", 7, "skills/"}})
	if want := "packs/ skills/ prompts/"; strings.Join(got, " ") != want {
		t.Errorf("Roots = %v, want %s", got, want)
	}
	if got := Roots(nil); strings.Join(got, " ") != "packs/" {
		t.Errorf("an unset config is the shelf alone, got %v", got)
	}
}

func TestRootsOfARepoWithNoSettingsIsTheShelf(t *testing.T) {
	got, err := RootsOf(t.TempDir())
	if err != nil || strings.Join(got, " ") != "packs/" {
		t.Errorf("RootsOf = %v, %v", got, err)
	}
}
