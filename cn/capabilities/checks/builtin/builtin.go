package builtin

import (
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
)

// all are the registered built-ins, each file's added by its init: a
// check's Run names its own var for the findings it makes, so the two are
// joined once both exist.
var all []declared.Builtin

func register(b *declared.Builtin, run func(*declared.Ctx, *transcript.Session) []findings.Finding) {
	b.Run = run
	all = append(all, *b)
	provenance.RegisterEngineCheck(b.Pack, b.ID)
}

// All are the engine's own checks of the folded packs, by id, each
// running only where its pack is declared.
func All() []declared.Builtin {
	out := append([]declared.Builtin{}, all...)
	sort.Slice(out, func(i, k int) bool { return out[i].ID < out[k].ID })
	return out
}
