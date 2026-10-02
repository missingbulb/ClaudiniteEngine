package builtin

import (
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/growth"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// The write-surface gate for the growth runs whose surface is the repo's
// own local packs (extract, dedup, the two sweeps): extract's pull
// request merges with no review, so the boundary is a machine guarantee.
// A run marks itself by its pinned commit subject, so the check gates
// itself and runs wherever the pack is declared; with no merge base there
// is no branch to read, and it says nothing.
var growthWriteScope = declared.Builtin{
	ID:     "growth-write-scope",
	Pack:   "claudinite-growth",
	OnFail: "block",
	Tags:   []string{"work", "builtin", "claudinite-growth"},
	Doc:    "packs/claudinite-growth/README.md",
	Why:    "extract auto-merges its PR with no human review and the rest run unattended; every one of them improves the repo's own packs, so a write outside .claudinite/local/packs/ — the canon it reads against, or the project's own code — escapes the review-by-blast-radius boundary the growth lifecycle is built on",
}

func init() { register(&growthWriteScope, runGrowthWriteScope) }

func runGrowthWriteScope(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if onDefaultBranch(ctx) || !anyMatches(ctx.Commits(), growth.RunSubject.MatchString) {
		return nil
	}
	var out []findings.Finding
	for _, p := range unique(ctx.ChangedFiles(), ctx.Deleted()) {
		if strings.HasPrefix(p, growth.LocalPacks) {
			continue
		}
		out = append(out, growthWriteScope.Finding(p, 0, "a growth run touched "+p+", outside "+growth.LocalPacks,
			"a growth run improves the repo's own packs, never the canon or the project's code — keep the whole write surface inside the local packs; a site-tied lesson lands as the owning pack's entry naming the site, and the same action over a canon's packs/ shelf is a claudinite-canon-curation task's"))
	}
	return out
}

func anyMatches(xs []string, match func(string) bool) bool {
	for _, x := range xs {
		if match(x) {
			return true
		}
	}
	return false
}
