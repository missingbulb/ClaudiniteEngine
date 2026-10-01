package update

import (
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// States lists the versions held and revoked, each with its reason. Phase
// 2 fills it from npm's deprecation messages; phase 4 unions in the lists
// the license key carries.
type States struct {
	Held    map[string]string
	Revoked map[string]string
}

// StatesFromPackument reads the held: and revoked: deprecation messages.
func StatesFromPackument(p *npmreg.Packument) States {
	s := States{Held: map[string]string{}, Revoked: map[string]string{}}
	if p == nil {
		return s
	}
	for v, e := range p.Versions {
		switch d := npmreg.ParseDeprecation(e.Deprecated); d.Kind {
		case npmreg.Held:
			s.Held[v] = d.Reason
		case npmreg.Revoked:
			s.Revoked[v] = d.Reason
		}
	}
	return s
}

// Of is v's state, revoked before held, and its reason.
func (s States) Of(v string) (npmreg.Kind, string) {
	if r, ok := s.Revoked[v]; ok {
		return npmreg.Revoked, r
	}
	if r, ok := s.Held[v]; ok {
		return npmreg.Held, r
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
func Candidate(pin string, p *npmreg.Packument, s States) Choice {
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
		} else if d := npmreg.ParseDeprecation(e.Deprecated); d.Kind != "" {
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
