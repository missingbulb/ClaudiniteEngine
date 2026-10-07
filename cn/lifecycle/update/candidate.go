package update

import "github.com/missingbulb/ClaudiniteEngine/cn/shared/npmreg"

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
