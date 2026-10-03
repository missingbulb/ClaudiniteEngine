package update

import "github.com/missingbulb/ClaudiniteEngine/shared/npmreg"

// The engine candidate is shared/npmreg's, where the fleet's freshness
// reads it too.
type (
	States = npmreg.States
	Skip   = npmreg.Skip
	Choice = npmreg.Choice
)

// KeyReason is the reason a version held or revoked by the license key
// alone carries.
const KeyReason = npmreg.KeyReason

// StatesFromPackument reads the held: and revoked: deprecation messages.
func StatesFromPackument(p *npmreg.Packument) States { return npmreg.StatesFromPackument(p) }

// Candidate picks the newest version of the channel package that is newer
// than pin, not deprecated on npm and not held or revoked.
func Candidate(pin string, p *npmreg.Packument, s States) Choice { return npmreg.Candidate(pin, p, s) }
