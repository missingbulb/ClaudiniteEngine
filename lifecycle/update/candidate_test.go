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
		{"newest wins", "60930.1.0", packument(nil, "0.0.0", "60930.1.0", "60930.2.0", "60930.10.0"), States{}, "60930.10.0", "", ""},
		// strings never floats: 60930.10 sorts after 60930.9
		{"ordinal ten after nine", "60930.9.0", packument(nil, "60930.9.0", "60930.10.0"), States{}, "60930.10.0", "", ""},
		{"nothing newer", "60930.2.0", packument(nil, "60930.1.0", "60930.2.0"), States{}, "", "60930.2.0", "not newer"},
		{"held by states", "60930.1.0", packument(nil, "60930.1.0", "60930.2.0"), States{Held: map[string]string{"60930.2.0": "canary red"}}, "", "60930.2.0", "held"},
		{"revoked by states", "60930.1.0", packument(nil, "60930.1.0", "60930.2.0"), States{Revoked: map[string]string{"60930.2.0": "bad"}}, "", "60930.2.0", "revoked"},
		{"held through npm", "60930.1.0", packument(map[string]string{"60930.2.0": "held: x"}, "60930.1.0", "60930.2.0"), States{}, "", "60930.2.0", "held"},
		{"revoked through npm", "60930.1.0", packument(map[string]string{"60930.2.0": "revoked: x"}, "60930.1.0", "60930.2.0"), States{}, "", "60930.2.0", "revoked"},
		{"plain deprecation", "60930.1.0", packument(map[string]string{"60930.2.0": "do not use"}, "60930.1.0", "60930.2.0"), States{}, "", "60930.2.0", "deprecated"},
		{"skips a held newest for the next", "60930.1.0", packument(map[string]string{"60930.3.0": "held: x"}, "60930.1.0", "60930.2.0", "60930.3.0"), States{}, "60930.2.0", "60930.3.0", "held"},
		{"ignores an unparseable version", "60930.1.0", packument(nil, "60930.1.0", "1.2.3-beta", "60930.2.0"), States{}, "60930.2.0", "", ""},
		{"empty packument", "60930.1.0", packument(nil), States{}, "", "", ""},
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
