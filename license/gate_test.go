package license

import (
	"slices"
	"testing"
)

// The license design's "What runs in each state" table, row by row: the
// surface, then on under ok or grace, then on under degraded.
var designTable = []struct {
	surface      Surface
	ok, degraded bool
}{
	{SurfaceRules, true, true},
	{SurfaceSkills, true, true},
	{SurfaceForcedSkillLoading, true, false},
	{SurfaceGuards, true, true},
	{SurfaceWorkChecks, true, false},
	{SurfaceInSessionGrowth, true, false},
	{SurfaceCIChecks, true, true},
	{SurfaceProjectTasks, true, true},
	{SurfaceClaudiniteTasks, true, false},
	{SurfaceUpdates, true, false},
	{SurfaceFleet, true, false},
	{SurfaceKeyRequests, true, true},
}

func every() []string {
	return []string{"work-checks", "forced-skill-loading", "in-session-growth", "claudinite-tasks", "updates", "fleet"}
}

func TestGateFollowsTheDesignTable(t *testing.T) {
	if len(designTable) != len(Surfaces) {
		t.Fatalf("the table has %d rows, the gate %d surfaces", len(designTable), len(Surfaces))
	}
	for _, state := range []string{"ok", "grace", "unverified"} {
		k := minted(t, map[string]any{"state": state, "features": every()})
		g := Gate(&k, false)
		for _, row := range designTable {
			if g.On(row.surface) != row.ok {
				t.Errorf("%s: %s is %v, want %v", state, row.surface, g.On(row.surface), row.ok)
			}
		}
	}
	k := minted(t, map[string]any{"state": "degraded", "features": []string{}})
	for name, g := range map[string]Gates{"degraded key": Gate(&k, false), "no key, not pending": Gate(nil, false)} {
		for _, row := range designTable {
			if g.On(row.surface) != row.degraded {
				t.Errorf("%s: %s is %v, want %v", name, row.surface, g.On(row.surface), row.degraded)
			}
		}
	}
}

func TestPendingTurnsEverySurfaceOn(t *testing.T) {
	g := Gate(nil, true)
	for _, s := range Surfaces {
		if !g.On(s) {
			t.Errorf("%s off while pending", s)
		}
	}
}

func TestOnlyTheSixNamesTurnAFeatureOn(t *testing.T) {
	k := minted(t, map[string]any{"features": []string{"work-checks", "rules-plus", "fleet-manager"}})
	g := Gate(&k, false)
	if !g.On(SurfaceWorkChecks) || g.On(SurfaceFleet) || g.On(SurfaceUpdates) {
		t.Errorf("%v", g)
	}
	pub := minted(t, nil)
	if g := Gate(&pub, false); g.On(SurfaceFleet) || !g.On(SurfaceUpdates) {
		t.Errorf("a Public key: %v", g)
	}
}

func TestOnlyGrowthAndFleetAreStillStubs(t *testing.T) {
	if !slices.Equal(StubSurfaces, []Surface{SurfaceInSessionGrowth, SurfaceFleet}) {
		t.Errorf("stubs %v: the tasks and forced-loading rows have readers", StubSurfaces)
	}
}

func TestTheStubSurfacesAreRows(t *testing.T) {
	for _, s := range StubSurfaces {
		if !slices.Contains(Surfaces, s) {
			t.Errorf("stub %s is not a gate row", s)
		}
	}
}
