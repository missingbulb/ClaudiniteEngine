package builtin

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
)

// The world half of the provenance convention: every carrier of every
// pack under either root (a canon's shelf, a member's local packs) names
// a live provenance file, every file parses, and a file with entries
// opens with born and carries Mechanism where its kind demands. The mount
// is never scanned. A fault that repeats in one file is one finding naming
// the count, and pending history (an empty file) is one advisory per
// pack.
var provenanceIntegrity = declared.Builtin{
	ID:     "provenance-integrity",
	Pack:   "claudinite-growth",
	OnFail: "block",
	Since:  "2026-09-20",
	Tags:   []string{"world", "builtin", "claudinite-growth"},
	Doc:    "packs/claudinite-growth/skills/changing-pack-elements/SKILL.md",
	Why:    "a rule with no file has no id an override can name and no log a review can reaffirm it against, and a file nothing names is history of an element that is gone",
}

func init() { register(&provenanceIntegrity, runProvenanceIntegrity) }

// growthTool is the provenance CLI a finding's remedy names.
const growthTool = "cn provenance"

// treeIO reads the working tree, its directories off the run's scanned
// files, so an audit walks exactly what the run scans.
func treeIO(ctx *declared.Ctx) provenance.IO {
	return provenance.ListIO{Files: ctx.Files(), ReadFile: ctx.Read, Has: ctx.Exists}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func runProvenanceIntegrity(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	packs := provenance.PackDirsIn(ctx.Files())
	if len(packs) == 0 {
		return nil
	}
	io := treeIO(ctx)
	tool := growthTool
	var out []findings.Finding
	b := provenanceIntegrity
	for _, dir := range packs {
		a := provenance.AuditPack(dir, io)
		id := path.Base(dir)
		mark := fmt.Sprintf("run `%s mark %s` and refine the proposed slugs before landing", tool, id)
		var order []string
		byFile := map[string][]provenance.Rule{}
		for _, u := range a.Unmarked {
			if _, ok := byFile[u.File]; !ok {
				order = append(order, u.File)
			}
			byFile[u.File] = append(byFile[u.File], u)
		}
		for _, f := range order {
			list := byFile[f]
			out = append(out, b.Finding(f, list[0].Line,
				fmt.Sprintf(`%d %s with no marker naming a provenance file (first: "%s")`, len(list), plural(len(list), "rule ends", "rules end"), list[0].Trigger), mark))
		}
		for _, d := range a.Dangling {
			what, fix := "not a file", fmt.Sprintf("create the file the carrier names (`%s mark %s` creates every missing one), or fix the marker to the file it meant", tool, id)
			if d.Retired {
				what, fix = "retired - its last entry retired the element", "a retired element carries no live carrier: remove the carrier, or give the element a new file and a born entry"
			}
			out = append(out, b.Finding(d.File, d.Line, fmt.Sprintf("%s names %s/%s/%s, which is %s", d.Carrier, dir, provenance.Dir, provenance.FileOfID(d.ID), what), fix))
		}
		for _, u := range a.Unnamed {
			out = append(out, b.Finding(u.Path, 0, fmt.Sprintf("is live, and no carrier of %s names %s", dir, provenance.FileOfID(u.ID)),
				"append a retired entry if the element is gone, or restore the marker or id that named it"))
		}
		for _, n := range a.NoBody {
			out = append(out, b.Finding(n.File, 0, "skill "+n.Name+" declares no body",
				fmt.Sprintf("add `body: workflow` or `body: guidelines` under its frontmatter metadata (`%s mark %s` proposes one from the shape)", tool, id)))
		}
		for _, m := range a.MarkerInWorkflow {
			out = append(out, b.Finding(m.File, m.LastLine, fmt.Sprintf(`"%s" ends with the marker (%s) inside a skill whose body is a workflow`, m.Trigger, m.Slug),
				"a workflow's steps owe their entries to the skill's own file: drop the marker, or declare the skill body: guidelines"))
		}
		for _, e := range append(append([]provenance.Fault{}, a.ParseErrors...), a.EntryFaults...) {
			out = append(out, b.Finding(e.File, e.Line, e.What,
				"write the entry in the file grammar - `## <YYYY-MM-DD> · <kind> · <one line>` and `- **Field:** …` lines, appended in date order, born first, Mechanism on every mechanism-bearing kind - through `append`, which validates it"))
		}
		if len(a.Empty) > 0 {
			var ids []string
			for _, e := range a.Empty {
				ids = append(ids, provenance.FileOfID(e.ID))
			}
			sort.Strings(ids)
			shown := ids
			more := ""
			if len(ids) > 3 {
				shown, more = ids[:3], ", …"
			}
			out = append(out, b.Advice(dir+"/"+provenance.Dir, 0,
				fmt.Sprintf("%d provenance %s empty - elements whose history is not written yet (%s%s)", len(ids), plural(len(ids), "file is", "files are"), strings.Join(shown, ", "), more),
				"the backfill fills them from each carrier's history (the backfilling-provenance skill), which says how a run is sized into pull requests; nothing else is owed"))
		}
	}
	return out
}
