package items

import _ "embed"

// The routine session's two procedures, shipped with the commands they
// name: the run itself, which a routine's stored prompt has it print, and
// the agent's half of the landing lane, which validation prints for a task
// whose outcome delivers a pull request.
var (
	//go:embed procedures/instructions.md
	RoutineInstructions string
	//go:embed procedures/deliver-pr.md
	DeliveryProcedure string
)
