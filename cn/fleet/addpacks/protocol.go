package addpacks

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsregex"
)

// The work-list protocol between this sweep, which writes the issues,
// and each member's own adopt-requested-packs task, which reads them. The
// member half carries its own copy in the lifecycle pack; that copy's
// test holds it to `cn fleet protocol --json`, which prints these.
const (
	// RequestedTitle is the list a person asked for: the member adopts
	// what its JSON block says, verbatim.
	RequestedTitle = "Add packs: requested for this repo"
	// SuspectedTitle is the list the scan fingerprinted: the member's
	// agent confirms each one against its checkout first.
	SuspectedTitle = "Add packs: suspected from this repo’s shape"
	// Mark is the label that makes the member's own scheduler adopt the
	// issue as a work item.
	Mark = workitem.OriginAdHoc
	// MemberTaskID is the task the issue's Task: field names.
	MemberTaskID = "claudinite-lifecycle/adopt-requested-packs"
)

// IsWorkListTitle says whether title is one of the two work lists: the
// title is the list's key, in the member and in every sweep.
func IsWorkListTitle(title string) bool { return title == RequestedTitle || title == SuspectedTitle }

// Protocol is the constants as `cn fleet protocol --json` prints them.
func Protocol() map[string]string {
	return map[string]string{"requestedTitle": RequestedTitle, "suspectedTitle": SuspectedTitle, "mark": Mark, "memberTaskId": MemberTaskID}
}

var blockJSON = regexp.MustCompile("(?s)```json\n(.*?)\n```")

// EntriesIn is the declaration entries a requested issue's fenced JSON
// block carries, nil when the block is absent, unparsable, not an array
// or names no id: nil is "leave it for a human", never an empty request.
func EntriesIn(body string) []any {
	m := blockJSON.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	var entries []any
	if err := json.Unmarshal([]byte(m[1]), &entries); err != nil || entries == nil {
		return nil
	}
	for _, e := range entries {
		if o, ok := e.(map[string]any); ok {
			if _, ok := o["id"].(string); ok {
				return entries
			}
		}
	}
	return nil
}

// EntryIDs is each entry's string id.
func EntryIDs(entries []any) []string {
	var out []string
	for _, e := range entries {
		if o, ok := e.(map[string]any); ok {
			if id, ok := o["id"].(string); ok {
				out = append(out, id)
				continue
			}
		}
		out = append(out, "")
	}
	return out
}

// WithTargeting is body with the fields that make it a request: the task
// that drains it and, where the member has another open work list, what
// it waits on (0 for none).
func WithTargeting(body string, blockedBy int) string {
	fields := []string{"Task: " + MemberTaskID}
	if blockedBy != 0 {
		fields = append(fields, fmt.Sprintf("Blocked-by: #%d", blockedBy))
	}
	return strings.Join(fields, "\n") + "\n\n" + strings.TrimLeftFunc(body, jsregex.IsSpace)
}
