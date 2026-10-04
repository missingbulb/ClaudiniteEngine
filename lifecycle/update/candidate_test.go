package update

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
)

func packument(deprecated map[string]string, versions ...string) *npmreg.Packument {
	p := &npmreg.Packument{Versions: map[string]npmreg.Version{}}
	for _, v := range versions {
		var e npmreg.Version
		e.Version, e.Deprecated = v, deprecated[v]
		p.Versions[v] = e
	}
	return p
}

func TestCandidate(t *testing.T) {
	cases := []struct {
		name       string
		pin        string
		p          *npmreg.Packument
		states     States
		want       string
		skipped    string
		skipReason string
	}{
		{"newest wins", "1.60930.1", packument(nil, "0.0.0", "1.60930.1", "1.60930.2", "1.60930.10"), States{}, "1.60930.10", "", ""},
		// strings never floats: 60930.10 sorts after 60930.9
		{"ordinal ten after nine", "1.60930.9", packument(nil, "1.60930.9", "1.60930.10"), States{}, "1.60930.10", "", ""},
		{"nothing newer", "1.60930.2", packument(nil, "1.60930.1", "1.60930.2"), States{}, "", "1.60930.2", "not newer"},
		{"held by states", "1.60930.1", packument(nil, "1.60930.1", "1.60930.2"), States{Held: map[string]string{"1.60930.2": "canary red"}}, "", "1.60930.2", "held"},
		{"revoked by states", "1.60930.1", packument(nil, "1.60930.1", "1.60930.2"), States{Revoked: map[string]string{"1.60930.2": "bad"}}, "", "1.60930.2", "revoked"},
		{"held through npm", "1.60930.1", packument(map[string]string{"1.60930.2": "held: x"}, "1.60930.1", "1.60930.2"), States{}, "", "1.60930.2", "held"},
		{"revoked through npm", "1.60930.1", packument(map[string]string{"1.60930.2": "revoked: x"}, "1.60930.1", "1.60930.2"), States{}, "", "1.60930.2", "revoked"},
		{"plain deprecation", "1.60930.1", packument(map[string]string{"1.60930.2": "do not use"}, "1.60930.1", "1.60930.2"), States{}, "", "1.60930.2", "deprecated"},
		{"skips a held newest for the next", "1.60930.1", packument(map[string]string{"1.60930.3": "held: x"}, "1.60930.1", "1.60930.2", "1.60930.3"), States{}, "1.60930.2", "1.60930.3", "held"},
		{"ignores an unparseable version", "1.60930.1", packument(nil, "1.60930.1", "1.2.3-beta", "1.60930.2"), States{}, "1.60930.2", "", ""},
		{"empty packument", "1.60930.1", packument(nil), States{}, "", "", ""},
	}
	for _, c := range cases {
		got := Candidate(c.pin, c.p, c.states)
		if got.Version != c.want {
			t.Errorf("%s: candidate %q, want %q", c.name, got.Version, c.want)
		}
		var sv, sr string
		if got.Skipped != nil {
			sv, sr = got.Skipped.Version, got.Skipped.Reason
		}
		if sv != c.skipped || sr != c.skipReason {
			t.Errorf("%s: skipped %q (%s), want %q (%s)", c.name, sv, sr, c.skipped, c.skipReason)
		}
	}
}

func TestStatesFromPackument(t *testing.T) {
	s := StatesFromPackument(packument(map[string]string{"1.1.0": "held: a", "1.2.0": "revoked: b", "1.3.0": "other"}, "1.1.0", "1.2.0", "1.3.0", "1.4.0"))
	if s.Held["1.1.0"] != "a" || s.Revoked["1.2.0"] != "b" || len(s.Held) != 1 || len(s.Revoked) != 1 {
		t.Errorf("%+v", s)
	}
	if k, _ := s.Of("1.2.0"); k != npmreg.Revoked {
		t.Errorf("Of 1.2.0 = %s", k)
	}
	if k, _ := s.Of("1.4.0"); k != "" {
		t.Errorf("Of 1.4.0 = %s", k)
	}
}
