package workitem

import _ "embed"

// RoutineInstructions is the routine session's procedure: the run itself
// and the agent's half of the landing lane.
//
//go:embed instructions.md
var RoutineInstructions string
