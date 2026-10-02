package main

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
)

func keyed(state string, features ...string) *license.ActionsOnce {
	k := &license.KeyPayload{Typ: "actions", Plan: license.PlanPublic, State: state, Features: features}
	return &license.ActionsOnce{Request: func() license.ActionsResult { return license.ActionsResult{Key: k, Wire: "W"} }}
}

func TestOnlyAnItemNeedingTheKeyParksWithoutOne(t *testing.T) {
	member, agentic, growth := sessionTaskOf("acme-pack", "none"), sessionTaskOf("acme-pack", "sonnet"), sessionTaskOf("claudinite-growth", "none")
	asked := 0
	none := &license.ActionsOnce{Request: func() license.ActionsResult {
		asked++
		return license.ActionsResult{Cause: license.CauseNoOIDC, Detail: "id-token: write is missing"}
	}}
	if n := taskLicense(none, member); n != "" || asked != 0 {
		t.Error("a member's agentless task runs keyless, and asks for nothing:", n, asked)
	}
	if n := taskLicense(none, agentic); n == "" {
		t.Error("an agentic task without a key parks")
	}
	if n := taskLicense(keyed("degraded", "claudinite-tasks"), agentic); n == "" {
		t.Error("a degraded key parks the agentic item")
	}
	if n := taskLicense(keyed("ok"), growth); !strings.Contains(n, "claudinite-tasks") {
		t.Error("an engine pack's task needs the claudinite-tasks row:", n)
	}
	if n := taskLicense(keyed("ok", "claudinite-tasks"), growth); n != "" {
		t.Error(n)
	}
	if n := taskLicense(keyed("ok"), agentic); n != "" {
		t.Error("a member's agentic task needs a sound key, not the engine's row:", n)
	}
}

func TestOnlyAPolicyThatAuthorizesALandingEntersTheLane(t *testing.T) {
	for _, c := range []struct {
		policy any
		want   bool
	}{{nil, false}, {"nothing", false}, {"anything", true}, {[]any{"layout"}, true}, {"reject:layout", false}} {
		if got := mayLand(c.policy); got != c.want {
			t.Errorf("%v: %v", c.policy, got)
		}
	}
}

func sessionTaskOf(pack, model string) taskspec.Task {
	return taskspec.Task{Pack: pack, ID: "a", Decl: taskspec.Decl{"id": "a", "agent_model": model}}
}
