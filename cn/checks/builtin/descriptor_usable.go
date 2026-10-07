package builtin

import (
	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/dashdesc"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// A pack's dashboard descriptor must be one the dashboard's own reader
// can use, with every id its views select declared. A descriptor the
// reader rejects does not break the page: it becomes one apologetic line
// on that pack's card in someone else's browser, so nothing goes red
// where the author looks unless this does. The scope is the descriptors
// a repo can fix, the shelf's packs/<id>/ and a member's own local packs,
// taken from every file the walk sees so an untracked one is covered;
// the vendored mount is the canon's to fix, never a member's.
var descriptorUsable = declared.Builtin{
	ID:     "descriptor-usable",
	Pack:   "claudinite-dashboard",
	OnFail: "block",
	Tags:   []string{"world", "builtin", "claudinite-dashboard"},
	Doc:    dashdesc.SchemaPath,
	Why:    "a descriptor the reader rejects renders as an apology on a card in someone else's browser — the pack ships, converges, and reports nothing, with nothing going red anywhere the author looks",
}

func init() { register(&descriptorUsable, runDescriptorUsable) }

func runDescriptorUsable(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	var out []findings.Finding
	for _, f := range dashdesc.Scan(ctx.AllFiles(), ctx.Read) {
		out = append(out, descriptorUsable.Finding(f.File, 0, f.What, f.Fix))
	}
	return out
}
