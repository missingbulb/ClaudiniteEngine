// Package recover continues a dead executor run. A run that dies never
// reaches its own re-dispatch; the job that runs after any such death
// dispatches a fresh one, bounded by the chain's depth, and past the bound
// says so on the repository's one failure issue. The dead run's own item
// is left to the leash.
package recover

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/schedule"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// MaxDepth is how many continuations a chain makes before it stops.
const MaxDepth = 3

// ChainFailureTitle is the failure issue a stopped chain reports on.
const ChainFailureTitle = "Claudinite executor chain failed repeatedly"

// DepthInput is the dispatch input carrying the chain's depth.
const DepthInput = "continuation_depth"

// NextDepth is the depth of the continuation after raw: absent, empty or
// unreadable is the first death, never a large number that would silence
// the chain when the queue most needs it.
func NextDepth(raw string) int {
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || n <= 0 {
		return 1
	}
	return int(n) + 1
}

// In is one continuation.
type In struct {
	Issues world.Issues
	// Dispatch fires the executor workflow on Branch with inputs.
	Dispatch func(inputs map[string]string) error
	Branch   string
	Depth    int
	RunURL   string
	Log      func(string)
}

// Continue dispatches the next link, or past MaxDepth reports the chain
// stopped and fails: the error is what turns the calling job red.
func Continue(in In) error {
	if in.Depth <= MaxDepth {
		if err := in.Dispatch(map[string]string{DepthInput: strconv.Itoa(in.Depth)}); err != nil {
			return fmt.Errorf("could not continue the chain on %s: %w", in.Branch, err)
		}
		in.Log(fmt.Sprintf("- the executor run died — dispatched a fresh one on %s (continuation %d of %d)", in.Branch, in.Depth, MaxDepth))
		return nil
	}
	body := fmt.Sprintf("%d consecutive executor runs died before finishing their item. The chain stops here; the next scheduled scheduler run will dispatch a fresh one.", MaxDepth)
	if in.RunURL != "" {
		body += "\n\nThe last run: " + in.RunURL
	}
	n, created, err := schedule.ReportFailure(in.Issues, schedule.FailureLabels, ChainFailureTitle, body)
	if err != nil {
		return fmt.Errorf("the executor chain died %d times in a row, and the failure issue could not be written: %w", MaxDepth, err)
	}
	verb := "commented on"
	if created {
		verb = "filed"
	}
	in.Log(fmt.Sprintf("- %s #%d", verb, n))
	return fmt.Errorf("the executor chain died %d times in a row — escalated on #%d", MaxDepth, n)
}
