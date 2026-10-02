package execute

import "testing"

func TestWhichItemsNeedTheRunsKey(t *testing.T) {
	growth := agentless("fold")
	growth.Pack = "claudinite-growth"
	builtin := loopTask("implement-request", nil)
	builtin.Pack = "engine"
	for _, c := range []struct {
		name            string
		task            func() KeyNeed
		agentic, engine bool
	}{
		{"a member's agentless task", func() KeyNeed { return KeyNeedOf(agentless("a")) }, false, false},
		{"a member's agentic task", func() KeyNeed { return KeyNeedOf(loopTask("a", nil)) }, true, false},
		{"an engine pack's agentless task", func() KeyNeed { return KeyNeedOf(growth) }, false, true},
		{"the built-in implementer", func() KeyNeed { return KeyNeedOf(builtin) }, true, true},
	} {
		n := c.task()
		if n.Agentic != c.agentic || n.EnginePack != c.engine || n.Any() != (c.agentic || c.engine) {
			t.Errorf("%s: %+v", c.name, n)
		}
	}
}
