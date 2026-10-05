package update

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
)

// packument holds versions, tags naming the dist-tags that point at them.
func packument(deprecated, tags map[string]string, versions ...string) *npmreg.Packument {
	p := &npmreg.Packument{Versions: map[string]npmreg.Version{}, DistTags: tags}
	for _, v := range versions {
		var e npmreg.Version
		e.Version, e.Deprecated = v, deprecated[v]
		p.Versions[v] = e
	}
	return p
}

func TestCandidate(t *testing.T) {
	t.Parallel()
	all := []string{"0.0.0", "1.60930.1", "1.60930.2", "1.60930.3", "1.60930.9", "1.60930.10"}
	tags := func(latest, rc, staging string) map[string]string {
		m := map[string]string{}
		for k, v := range map[string]string{"latest": latest, "rc": rc, "staging": staging} {
			if v != "" {
				m[k] = v
			}
		}
		return m
	}
	cases := []struct {
		name       string
		pin        string
		channel    string
		p          *npmreg.Packument
		states     States
		want       string
		skipped    string
		skipReason string
	}{
		{"stable takes latest", "1.60930.1", "stable", packument(nil, tags("1.60930.2", "1.60930.3", "1.60930.9"), all...), States{}, "1.60930.2", "", ""},
		{"canary takes rc", "1.60930.1", "canary", packument(nil, tags("1.60930.2", "1.60930.3", "1.60930.9"), all...), States{}, "1.60930.3", "", ""},
		{"staging takes staging", "1.60930.1", "staging", packument(nil, tags("1.60930.2", "1.60930.3", "1.60930.9"), all...), States{}, "1.60930.9", "", ""},
		{"canary takes a promotion newer than rc", "1.60930.1", "canary", packument(nil, tags("1.60930.3", "1.60930.2", ""), all...), States{}, "1.60930.3", "", ""},
		{"staging takes an rc newer than staging", "1.60930.1", "staging", packument(nil, tags("1.60930.2", "1.60930.9", "1.60930.3"), all...), States{}, "1.60930.9", "", ""},
		{"an untagged newer version is never taken", "1.60930.1", "staging", packument(nil, tags("1.60930.2", "", ""), all...), States{}, "1.60930.2", "", ""},
		// strings never floats: 60930.10 sorts after 60930.9
		{"ordinal ten after nine", "1.60930.9", "canary", packument(nil, tags("1.60930.9", "1.60930.10", ""), all...), States{}, "1.60930.10", "", ""},
		{"nothing newer", "1.60930.2", "stable", packument(nil, tags("1.60930.2", "", ""), all...), States{}, "", "1.60930.2", "not newer"},
		{"held by states", "1.60930.1", "stable", packument(nil, tags("1.60930.2", "", ""), all...), States{Held: map[string]string{"1.60930.2": "canary red"}}, "", "1.60930.2", "held"},
		{"revoked by states", "1.60930.1", "stable", packument(nil, tags("1.60930.2", "", ""), all...), States{Revoked: map[string]string{"1.60930.2": "bad"}}, "", "1.60930.2", "revoked"},
		{"held through npm", "1.60930.1", "stable", packument(map[string]string{"1.60930.2": "held: x"}, tags("1.60930.2", "", ""), all...), States{}, "", "1.60930.2", "held"},
		{"revoked through npm", "1.60930.1", "stable", packument(map[string]string{"1.60930.2": "revoked: x"}, tags("1.60930.2", "", ""), all...), States{}, "", "1.60930.2", "revoked"},
		{"plain deprecation", "1.60930.1", "stable", packument(map[string]string{"1.60930.2": "do not use"}, tags("1.60930.2", "", ""), all...), States{}, "", "1.60930.2", "deprecated"},
		{"skips a held rc for latest", "1.60930.1", "canary", packument(map[string]string{"1.60930.3": "held: x"}, tags("1.60930.2", "1.60930.3", ""), all...), States{}, "1.60930.2", "1.60930.3", "held"},
		{"ignores an unparseable tagged version", "1.60930.1", "staging", packument(nil, tags("1.60930.2", "", "1.2.3-beta"), append(all, "1.2.3-beta")...), States{}, "1.60930.2", "", ""},
		{"unknown channel takes nothing", "1.60930.1", "beta", packument(nil, tags("1.60930.2", "", ""), all...), States{}, "", "", ""},
		{"empty packument", "1.60930.1", "stable", packument(nil, nil), States{}, "", "", ""},
	}
	for _, c := range cases {
		got := Candidate(c.pin, c.channel, c.p, c.states)
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
	t.Parallel()
	s := StatesFromPackument(packument(map[string]string{"1.1.0": "held: a", "1.2.0": "revoked: b", "1.3.0": "other"}, nil, "1.1.0", "1.2.0", "1.3.0", "1.4.0"))
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
