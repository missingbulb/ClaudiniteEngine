package adopt

import (
	"fmt"
	"io"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
)

// Step is one HANDOVER row: a step only a person can do, who it is for,
// what breaks while it is undone and what shows it is done. cn performs
// none of them.
type Step struct {
	Owner, Step, Breaks, Done string
}

// HandoverInput is what the block is built from.
type HandoverInput struct {
	// Core adds the rows every adoption owes: the Actions setting and,
	// when no key came, the App install or the plan.
	Core bool
	Key  KeyGrant
	// Tasks adds the executor routine's token row.
	Tasks bool
	// Newly are the packs this run vendored, whose adoptionHandover steps
	// follow the core rows in order.
	Newly []packset.Pack
}

// Handover is the block's rows.
func Handover(in HandoverInput) []Step {
	var out []Step
	if in.Core {
		out = append(out, Step{"cn",
			"In the repository's Settings > Actions > General, allow GitHub Actions to create and approve pull requests",
			"the nightly update and every task that opens a pull request fail at the open",
			"the first engine/update item's pull request exists"})
		switch {
		case in.Key.Plan != "":
		case in.Key.Checkout != "":
			out = append(out, Step{"cn",
				"Pick a plan for this private repo: " + in.Key.Checkout,
				"sessions run degraded (" + in.Key.Reason + "); the Public plan covers public repos only",
				"a session's SessionStart reports a key naming the plan"})
		default:
			link := in.Key.Link
			if link == "" {
				link = InstallURL
			}
			out = append(out, Step{"cn",
				"Install the Claudinite GitHub App on this account: " + link,
				"sessions run degraded (no license key came: " + in.Key.Reason + ")",
				"a session's SessionStart reports a key"})
		}
	}
	if in.Tasks {
		out = append(out, Step{workitem.TasksPackID,
			"Mint the executor routine's bearer token and add it as the Actions secret CCR_ROUTINE_TOKEN",
			"every item with an agentic phase parks needs-human-action naming the secret",
			"an executor run fires a routine"})
	}
	for _, p := range in.Newly {
		for _, h := range p.Manifest.Handover {
			out = append(out, Step{p.Token(), oneLine(h.Step), oneLine(h.Breaks), oneLine(h.Done)})
		}
	}
	return out
}

// writeHandover prints the HANDOVER block, nothing when there is no row.
func writeHandover(out io.Writer, steps []Step) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintf(out, "\nHANDOVER — %d step(s) only a human can do; file them as ONE issue, a checkbox each, never a note in the PR body:\n", len(steps))
	for _, s := range steps {
		fmt.Fprintf(out, "  [ ] (%s) %s\n        while off: %s\n        done when: %s\n", s.Owner, s.Step, s.Breaks, s.Done)
	}
}

// NextInput is what the NEXT line says.
type NextInput struct {
	// First are the steps before the commit's, in order.
	First []string
	// Routine is the executor routine's step, where claudinite-tasks is
	// declared.
	Routine  bool
	Handover bool
}

// writeNext prints the one NEXT line.
func writeNext(out io.Writer, in NextInput) {
	steps := append([]string{}, in.First...)
	if in.Routine {
		steps = append(steps, "create the executor routine with create_trigger and record its endpoint on the "+workitem.TasksPackID+" entry's config.agenticTaskInvocationEndpoints")
	}
	steps = append(steps, "commit everything above and open one pull request, which a person merges")
	if in.Handover {
		steps = append(steps, "file the HANDOVER block as one issue")
	}
	fmt.Fprintf(out, "\nNEXT: %s.\n", strings.Join(steps, "; then "))
}
