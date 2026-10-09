package npmreg

import (
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// States lists the versions held and revoked, each with its reason, as
// npm's deprecation messages say them.
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

// Tags are the npm dist-tags an engine channel takes releases from: its
// own and every more stable one's, so a canary repo also takes a promoted
// release newer than the last candidate. An unknown channel takes none.
func Tags(channel string) []string {
	switch channel {
	case settings.ChannelStable:
		return []string{"latest"}
	case settings.ChannelCanary:
		return []string{"latest", "rc"}
	case settings.ChannelStaging:
		return []string{"latest", "rc", "staging"}
	}
	return nil
}

// Candidate picks, among the versions channel's tags point at, the newest
// that is newer than pin, not deprecated on npm and not held or revoked.
// A version no tag points at is never a candidate.
func Candidate(pin, channel string, p *Packument, s States) Choice {
	var c Choice
	newer := func(a, b string) bool {
		n, err := version.Compare(a, b)
		return err == nil && n > 0
	}
	if p == nil {
		return c
	}
	tagged := map[string]bool{}
	for _, t := range Tags(channel) {
		if v := p.DistTags[t]; v != "" {
			tagged[v] = true
		}
	}
	for v, e := range p.Versions {
		if _, err := version.Parse(v); err != nil || !tagged[v] {
			continue
		}
		reason := ""
		if k, _ := s.Of(v); k != "" {
			reason = string(k)
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
