package execute

import "testing"

func TestWhichItemsNeedTheRunsKey(t *testing.T) {
	growth := agentless("fold")
	growth.Pack, growth.Engine = "acme-pack", true
	named := agentless("fold")
	named.Pack = "claudinite-growth" // @real-entity a pack's name alone does not make its tasks the engine's
	builtin := loopTask("implement-request", nil)
	builtin.Pack, builtin.Engine = "engine", true
	update := agentless("update")
	update.Pack, update.Engine = "engine", true
	for _, c := range []struct {
		name            string
		task            func() KeyNeed
		agentic, engine bool
	}{
		{"a member's agentless task", func() KeyNeed { return KeyNeedOf(agentless("a")) }, false, false},
		{"a member's agentic task", func() KeyNeed { return KeyNeedOf(loopTask("a", nil)) }, true, false},
		{"an engine pack's agentless task", func() KeyNeed { return KeyNeedOf(growth) }, false, true},
		{"a task whose pack only bears an engine pack's name", func() KeyNeed { return KeyNeedOf(named) }, false, false},
		{"the built-in update, whose own gate reads the key", func() KeyNeed { return KeyNeedOf(update) }, false, false},
		{"the built-in implementer", func() KeyNeed { return KeyNeedOf(builtin) }, true, true},
	} {
		n := c.task()
		if n.Agentic != c.agentic || n.EnginePack != c.engine || n.Any() != (c.agentic || c.engine) {
			t.Errorf("%s: %+v", c.name, n)
		}
	}
}
