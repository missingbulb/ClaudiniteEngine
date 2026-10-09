package queue

import (
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/world"
)

// SwapStatus moves an item out of a status, every spelling any engine
// wrote of it removed whatever the caller's snapshot shows, and into the
// label to: an item outlives its engine, and a swap naming one spelling
// leaves the other standing.
func SwapStatus(gh world.Issues, number int, from, to string) error {
	if err := ClearStatus(gh, number, from); err != nil {
		return err
	}
	return gh.AddLabel(number, to)
}

// ClearStatus leaves a status without entering another.
func ClearStatus(gh world.Issues, number int, status string) error {
	for _, l := range workitem.SpellingsOf(status) {
		if err := gh.RemoveLabel(number, l); err != nil {
			return err
		}
	}
	return nil
}
