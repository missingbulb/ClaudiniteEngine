package builtin

import (
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/curation"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// On a branch whose name carries growth-promote, every path the change
// touches since its merge base lies under the canon's corpus roots, and a
// branch with no merge base is refused rather than certified. A work
// check, so it runs at the promote session's Stop; any other branch
// writes anything.
var promoteScope = declared.Builtin{
	ID:     "promote-scope",
	Pack:   packset.FleetPack,
	OnFail: "block",
	Tags:   []string{"work", "builtin", packset.FleetPack},
	Since:  "2026-10-07",
	Doc:    "cn fleet promote-scope",
	Why:    "promote runs unattended with a fleet-wide token; a write outside the corpus roots escapes the review-by-blast-radius boundary the growth lifecycle is built on",
}

func init() { register(&promoteScope, runPromoteScope) }

func runPromoteScope(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if !strings.Contains(ctx.Branch(), curation.PromoteBranch) {
		return nil
	}
	if ctx.MergeBase() == "" {
		return []findings.Finding{promoteScope.Finding("", 0, "the promote branch has no merge base with the base branch, so its diff cannot be scoped to the corpus roots",
			"branch the promote run from the canon's default branch, so what it touched can be measured")}
	}
	roots := curation.Roots(ctx.Config.Fleet)
	names := make([]string, len(roots))
	for i, r := range roots {
		names[i] = strings.TrimSuffix(r, "/")
	}
	var out []findings.Finding
	for _, p := range curation.Stray(roots, append(append(ctx.ChangedFiles(), ctx.Deleted()...), ctx.UntrackedList()...)) {
		out = append(out, promoteScope.Finding(p, 0, "the promote phase touched "+p+", outside "+strings.Join(names, " and "),
			"a promoted lesson is portable canon — home it in a pack (prose or checks) or a skill the corpus carries; a lesson that can only live outside the corpus is out of promote scope, so leave it local"))
	}
	return out
}
