package license

import (
	"slices"
	"testing"
)

func TestReleaseStates(t *testing.T) {
	k := minted(t, map[string]any{"release": map[string]any{"held": []string{"60930.2.0"}, "revoked": []string{"60930.1.0"},
		"security_fixes": []string{"60930.3.0"}, "pack_index_serial": 41, "pack_keys": []string{"0123456789abcdef"}}})
	s := ReleaseStates(&k)
	if s.Held["60930.2.0"] != "license key" || s.Revoked["60930.1.0"] != "license key" || s.SerialFloor != 41 ||
		!slices.Equal(s.PackKeys, []string{"0123456789abcdef"}) || !slices.Equal(s.SecurityFixes, []string{"60930.3.0"}) {
		t.Errorf("%+v", s)
	}
	none := ReleaseStates(nil)
	if len(none.Held) != 0 || len(none.Revoked) != 0 || none.SerialFloor != 0 || len(none.PackKeys) != 0 {
		t.Errorf("nil key: %+v", none)
	}
}
