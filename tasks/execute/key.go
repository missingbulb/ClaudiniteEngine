package execute

import (
	"slices"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
)

// EnginePacks are the packs whose tasks are the engine's own, run under
// the license's claudinite-tasks row; every other pack's are the member's.
var EnginePacks = []string{"claudinite-growth", "claudinite-lifecycle", taskspec.BuiltinPack}

// KeyNeed is why an item needs the run's license key: an agentic phase,
// whose routine session verifies a grant asked for with the key, or a
// task of the engine's own packs. An item needing neither runs in every
// license state.
type KeyNeed struct {
	Agentic, EnginePack bool
}

// Any reports whether the item needs the key at all.
func (n KeyNeed) Any() bool { return n.Agentic || n.EnginePack }

// KeyNeedOf is a task's need.
func KeyNeedOf(t taskspec.Task) KeyNeed {
	return KeyNeed{Agentic: t.Decl.AgentModel() != "none", EnginePack: slices.Contains(EnginePacks, t.Pack)}
}
