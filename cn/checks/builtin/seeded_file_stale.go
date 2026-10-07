package builtin

import (
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// A seedOps file is written once, at adoption, and the member owns it
// from then on, so a pack that reshapes its template leaves every member
// running the adoption-era copy. The member is told, not repaired: its
// copy must carry every significant line of the template its own mount
// ships (comments, blanks and indentation dropped); lines it added are
// its business. Advisory: the remedy is often a person-merged change to
// .github/workflows/.
var seededFileStale = declared.Builtin{
	ID:     "seeded-file-stale",
	Pack:   "claudinite-lifecycle",
	OnFail: "advise",
	Tags:   []string{"world", "builtin", "claudinite-lifecycle"},
	Doc:    "packs/claudinite-lifecycle/README.md",
	Why:    "a seeded file is written once and never converged, so a pack that reshapes its template leaves every existing member running the adoption-era copy — which fails wherever the pack has since moved, with nothing anywhere saying so",
}

func init() { register(&seededFileStale, runSeededFileStale) }

func significantLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimFunc(l, checksdk.IsJSSpace)
		if l != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "//") {
			out = append(out, l)
		}
	}
	return out
}

func runSeededFileStale(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	var out []findings.Finding
	for _, p := range ctx.Config.Packs {
		for _, op := range p.Manifest.SeedOps {
			from := p.Rel + "/" + op.Template
			want, ok := ctx.Read(from)
			if !ok {
				continue
			}
			have, ok := ctx.Read(op.Dest)
			if !ok {
				continue
			}
			carried := map[string]bool{}
			for _, l := range significantLines(have) {
				carried[l] = true
			}
			var missing []string
			for _, l := range significantLines(want) {
				if !carried[l] {
					missing = append(missing, l)
				}
			}
			if len(missing) == 0 {
				continue
			}
			count := "a line"
			if len(missing) > 1 {
				count = strconv.Itoa(len(missing)) + " lines"
			}
			fix := "re-seed it — `cp " + from + " " + op.Dest + "` — then re-apply whatever this repo deliberately changed in its copy"
			if strings.HasPrefix(op.Dest, ".github/workflows/") {
				fix += ", and get that PR merged: an update cannot push to .github/workflows/, which is why nothing delivered the change"
			}
			out = append(out, seededFileStale.Finding(op.Dest, 0,
				"was seeded from "+p.ID+"'s "+op.Template+" and no longer carries "+count+" that template has — the first is `"+missing[0]+"`", fix))
		}
	}
	return out
}
