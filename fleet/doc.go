// Package fleet is the sheepdog's sweeps as engine code: a fleet manager
// (the repository declaring claudinite-fleet-sheepdog) reads every
// repository its owner owns over FLEET_GITHUB_TOKEN and answers, of each,
// whether it is a member and whether that membership still means
// anything, and dispatches each member's own update.
//
// It is cut on the line every fleet question already draws. The
// per-repo half reads one repository and judges it, knowing nothing of
// any other: ReadMember, Classify, Judge. The aggregation half enumerates
// the owner's repositories at run time and sums the per-repo answers:
// fleet/roster and fleet/update. No repository list lives in code; the
// fleet is the manager's owner and exclude list, resolved by enumeration.
//
// Toward members it reads, and dispatches their scheduler; every other
// write lands on the manager's own issues and labels. The package test
// lists each function that writes and holds the list to the source, so a
// new write toward a member is a reviewed edit to that list.
package fleet
