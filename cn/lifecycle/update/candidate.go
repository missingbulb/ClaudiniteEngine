package update

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/ghrelease"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// The engine candidate is shared/npmreg's.
type (
	States = npmreg.States
	Skip   = npmreg.Skip
	Choice = npmreg.Choice
)

// StatesFromPackument reads the held: and revoked: deprecation messages.
func StatesFromPackument(p *npmreg.Packument) States { return npmreg.StatesFromPackument(p) }

// Candidate picks the newest version channel's dist-tags point at that is
// newer than pin, not deprecated on npm and not held or revoked.
func Candidate(pin, channel string, p *npmreg.Packument, s States) Choice {
	return npmreg.Candidate(pin, channel, p, s)
}

// candidateOf is the version to move pin to: from npm's dist-tags as
// Candidate picks it, or, for a pin on engine.releases, the version that
// repository's latest release.json names when it is newer than the pin.
func candidateOf(d Deps, pin settings.Engine, p *npmreg.Packument, s States) (Choice, error) {
	if pin.Releases == "" {
		return Candidate(pin.Version, pin.Channel, p, s), nil
	}
	raw, err := d.Registry.Download(ghrelease.LatestURL(d.ReleasesHost, pin.Releases))
	if err != nil {
		return Choice{}, fmt.Errorf("the latest release of %s: %w", pin.Releases, err)
	}
	l, err := ghrelease.ParseLatest(raw)
	if err != nil {
		return Choice{}, fmt.Errorf("the latest release of %s: %w", pin.Releases, err)
	}
	if n, err := version.Compare(l.Version, pin.Version); err != nil || n <= 0 {
		return Choice{}, nil
	}
	return Choice{Version: l.Version}, nil
}
