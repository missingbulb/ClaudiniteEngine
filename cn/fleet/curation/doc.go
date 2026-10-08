// Package curation is a fleet manager's canon curation, over the packs/
// shelf its checkout keeps: the promote stage's write-surface gate, and
// the shelf's version walk, which answers which pull requests each pack
// version shipped. Both read the manager's own checkout and nothing of any
// member.
package curation

// Shelf is the tree a canon's packs live under, and the corpus root no
// config removes: a canon with no shelf is not a canon.
const Shelf = "packs"
