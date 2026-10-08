// Package interview is the adoption interview's state over a loaded pack
// set: the questions each active pack asks that its entry has not
// answered, and the answers an entry stores for a question its pack no
// longer asks. It is pure; init, adopt, SessionStart and the lifecycle
// pack's built-in checks read it.
package interview

import (
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
)

// Pending is one active pack's unanswered questions, in declared order.
type Pending struct {
	Pack      packset.Pack
	Questions []packset.Question
}

// Stale is a stored answer whose question its pack no longer declares.
type Stale struct {
	Pack   packset.Pack
	Answer string
}

// State reads the interview over set: an answered question stays
// answered, "n/a" included. A pack whose questions did not validate did not load, so it
// asks nothing and none of its answers is stale.
func State(set packset.Set) ([]Pending, []Stale) {
	var pending []Pending
	var stale []Stale
	for _, p := range set.Packs {
		if p.Kind == packset.Temp {
			continue
		}
		entry, _ := set.Declared.Entry(p.ID, p.Kind == packset.Local)
		var open []packset.Question
		asked := map[string]bool{}
		for _, q := range p.Manifest.Questions {
			asked[q.ID] = true
			if _, ok := entry.Answers[q.ID]; !ok {
				open = append(open, q)
			}
		}
		if len(open) > 0 {
			pending = append(pending, Pending{Pack: p, Questions: open})
		}
		var ids []string
		for id := range entry.Answers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if !asked[id] {
				stale = append(stale, Stale{Pack: p, Answer: id})
			}
		}
	}
	return pending, stale
}

// Count is the number of unanswered questions.
func Count(pending []Pending) int {
	n := 0
	for _, p := range pending {
		n += len(p.Questions)
	}
	return n
}
