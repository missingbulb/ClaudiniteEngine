package license

import "slices"

// Surface is one thing the binary gates by license state.
type Surface string

// The surfaces of the license design's "What runs in each state" table.
const (
	SurfaceRules              Surface = "rules"
	SurfaceSkills             Surface = "skills"
	SurfaceForcedSkillLoading Surface = "forced-skill-loading"
	SurfaceGuards             Surface = "guards"
	SurfaceWorkChecks         Surface = "work-checks"
	SurfaceInSessionGrowth    Surface = "in-session-growth"
	SurfaceCIChecks           Surface = "ci-checks"
	SurfaceProjectTasks       Surface = "project-tasks"
	SurfaceClaudiniteTasks    Surface = "claudinite-tasks"
	SurfaceUpdates            Surface = "updates"
	SurfaceFleet              Surface = "fleet"
	SurfaceKeyRequests        Surface = "key-requests"
)

// Surfaces is every row of the gate, in the design table's order.
var Surfaces = []Surface{SurfaceRules, SurfaceSkills, SurfaceForcedSkillLoading, SurfaceGuards, SurfaceWorkChecks,
	SurfaceInSessionGrowth, SurfaceCIChecks, SurfaceProjectTasks, SurfaceClaudiniteTasks, SurfaceUpdates, SurfaceFleet, SurfaceKeyRequests}

// alwaysOn run in every state, with or without a key.
var alwaysOn = []Surface{SurfaceRules, SurfaceSkills, SurfaceGuards, SurfaceCIChecks, SurfaceProjectTasks, SurfaceKeyRequests}

// StubSurfaces are rows no command reads yet: phase 6 wires their callers
// to the gate rather than inventing one.
var StubSurfaces = []Surface{SurfaceForcedSkillLoading, SurfaceInSessionGrowth, SurfaceClaudiniteTasks, SurfaceFleet}

// Gates is each surface on or off.
type Gates map[Surface]bool

// On reports whether s runs.
func (g Gates) On(s Surface) bool { return g[s] }

// Gate is the surfaces a key turns on. With no key, every surface is on
// while a request is pending (the first 10 seconds and the late tail) and
// only the always-on ones otherwise. A key turns on the always-on surfaces
// and each feature its list names; only the six feature surface names
// count and any other name is ignored, so ok, grace, unverified and
// degraded all gate by their own list.
func Gate(key *KeyPayload, pending bool) Gates {
	g := Gates{}
	for _, s := range Surfaces {
		switch {
		case slices.Contains(alwaysOn, s):
			g[s] = true
		case key == nil:
			g[s] = pending
		default:
			g[s] = slices.Contains(key.Features, string(s))
		}
	}
	return g
}
