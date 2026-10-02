// Package growth holds what the growth runs stamp and what the growth
// checks key on, spelled once: the commit subjects each run pins, and the
// fingerprints of a dedup edit that re-imports the canon. The values are
// the claudinite-growth pack's at missingbulb/Claudinite@057841ac,
// string-identical, so a check and the run it judges read one definition.
package growth

import "regexp"

// The commit subjects the growth runs pin, each the start of its run's
// title: extract (and the pre-merge title of extract's conversation
// half), dedup, and the two sweeps.
const (
	SubjectExtract             = "Claudinite growth: extract lessons"
	SubjectConversationExtract = "Claudinite growth: conversation extract"
	SubjectDedup               = "Claudinite growth: dedup local packs"
	SubjectProseToChecks       = "Claudinite growth: prose to checks"
	SubjectRuleRevalidation    = "Claudinite growth: rule revalidation"
)

// RunSubject matches a commit subject one of the runs whose write
// surface is the repo's own local packs stamped: the whole pinned title,
// never the "Claudinite growth:" prefix the lifecycle shares.
var RunSubject = regexp.MustCompile(`^Claudinite growth: (?:extract lessons|conversation extract|dedup\b|prose to checks|rule revalidation)`)

// DedupSubject matches a commit announcing a dedup.
var DedupSubject = regexp.MustCompile(`(?i)\bdedup\b|\bcanon now (?:covers|owns)\b`)

// RestatesCanon matches an added local-pack prose line that re-imports a
// canon rule: one saying a rule is portable (canon), that a pack or the
// canon owns it.
var RestatesCanon = regexp.MustCompile(`(?i)\b(?:is|are) portable\s*\(canon\)|\bpack owns\b|\bcanon (?:now )?owns\b|\bowned by (?:the )?canon\b`)

// LocalPacks is the write surface of every run RunSubject matches.
const LocalPacks = ".claudinite/local/packs/"
