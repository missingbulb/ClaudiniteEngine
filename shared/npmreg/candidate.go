package npmreg

import "github.com/missingbulb/ClaudiniteEngine/shared/version"

// KeyReason is the reason a version held or revoked by the license key
// alone carries.
const KeyReason = "license key"

// States lists the versions held and revoked, each with its reason. Phase
// 2 fills it from npm's deprecation messages; phase 4 unions in the lists
// the license key carries.
type States struct {
	Held    map[string]string
	Revoked map[string]string
}

// StatesFromPackument reads the held: and revoked: deprecation messages.
func StatesFromPackument(p *Packument) States {
	s := States{Held: map[string]string{}, Revoked: map[string]string{}}
	if p == nil {
		return s
	}
	for v, e := range p.Versions {
		switch d := ParseDeprecation(e.Deprecated); d.Kind {
		case Held:
			s.Held[v] = d.Reason
		case Revoked:
			s.Revoked[v] = d.Reason
		}
	}
	return s
}

// Of is v's state, revoked before held, and its reason.
func (s States) Of(v string) (Kind, string) {
	if r, ok := s.Revoked[v]; ok {
		return Revoked, r
	}
	if r, ok := s.Held[v]; ok {
		return Held, r
	}
	return "", ""
}

// Skip is a version the updater passed over and why: held, revoked,
// deprecated or not newer.
type Skip struct {
	Version, Reason string
}

// Choice is the version to move to, empty for none, and the newest version
// passed over above it.
type Choice struct {
	Version string
	Skipped *Skip
}

// Candidate picks the newest version of the channel package that is newer
// than pin, not deprecated on npm and not held or revoked.
func Candidate(pin string, p *Packument, s States) Choice {
	var c Choice
	newer := func(a, b string) bool {
		n, err := version.Compare(a, b)
		return err == nil && n > 0
	}
	for v, e := range p.Versions {
		if _, err := version.Parse(v); err != nil {
			continue
		}
		reason := ""
		if k, r := s.Of(v); k != "" {
			reason = skipReason(k, r)
		} else if d := ParseDeprecation(e.Deprecated); d.Kind != "" {
			reason = string(d.Kind)
		} else if !newer(v, pin) {
			reason = "not newer"
		}
		if reason == "" {
			if c.Version == "" || newer(v, c.Version) {
				c.Version = v
			}
			continue
		}
		if c.Skipped == nil || newer(v, c.Skipped.Version) {
			c.Skipped = &Skip{v, reason}
		}
	}
	if c.Skipped != nil && c.Version != "" && !newer(c.Skipped.Version, c.Version) {
		c.Skipped = nil
	}
	return c
}

// Union is s with o's entries added; s's reason stands where both name a
// version.
func (s States) Union(o States) States {
	out := States{Held: map[string]string{}, Revoked: map[string]string{}}
	for _, src := range []States{s, o} {
		for v, r := range src.Held {
			if _, ok := out.Held[v]; !ok {
				out.Held[v] = r
			}
		}
		for v, r := range src.Revoked {
			if _, ok := out.Revoked[v]; !ok {
				out.Revoked[v] = r
			}
		}
	}
	return out
}

// skipReason names a held or revoked state, marking one the key alone set.
func skipReason(k Kind, reason string) string {
	if reason == KeyReason {
		return string(k) + " (" + KeyReason + ")"
	}
	return string(k)
}
