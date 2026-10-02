package execute

import (
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
)

// KeyNeed is why an item needs the run's license key: an agentic phase,
// whose routine session verifies a grant asked for with the key, or one
// of the engine's own tasks (taskspec.Task.Engine), run under the
// license's claudinite-tasks row. An item needing neither runs in every
// license state.
type KeyNeed struct {
	Agentic, EnginePack bool
}

// Any reports whether the item needs the key at all.
func (n KeyNeed) Any() bool { return n.Agentic || n.EnginePack }

// KeyNeedOf is a task's need. The engine's update reads the key itself
// and says in its verdict what a missing or degraded one turns off, so it
// runs in every license state, as its workflow did.
func KeyNeedOf(t taskspec.Task) KeyNeed {
	selfGated := t.Pack == taskspec.BuiltinPack && t.ID == taskspec.UpdateTask
	return KeyNeed{Agentic: t.Decl.AgentModel() != "none", EnginePack: t.Engine && !selfGated}
}
