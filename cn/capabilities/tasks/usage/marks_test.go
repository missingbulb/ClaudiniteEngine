package usage

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// A mark a file holds as something other than text is compared the way
// JavaScript compares it: as a number, which no timestamp is, so nothing
// counts as already folded past it.
func TestAMarkThatIsNoTextFoldsEverythingPastIt(t *testing.T) {
	for _, mark := range []any{true, 7.0} {
		api := prsAPI(t, []*Obj{prJSON(`{"number": 9, "merged_at": "2026-08-20T10:00:00Z"}`)}, nil)
		if out := ReadMergedPrs(api, "o/r", mark, "2026-08-21T11:00:00Z"); len(out.Prs) != 1 || out.Watermark != "2026-08-20T10:00:00Z" {
			t.Errorf("merged PRs past %v: %+v", mark, out)
		}
		r := routes(t, map[string]string{workitem.SchedulerWorkflowFile: runsJSON(runJSON(1, "2026-08-21T09:00:00Z", "success"))})
		if runs, err := ReadWorkflowRuns(r, "o/r", workitem.SchedulerWorkflowFile, mark, 3); err != nil || len(runs) != 1 {
			t.Errorf("runs past %v: %+v %v", mark, runs, err)
		}
		if _, ok := QueueRecordFor(queueItem(t, `{}`), mark); !ok {
			t.Errorf("an item closed past %v", mark)
		}
	}
	if later, defined := jsLessThan("2026-08-20", "2026-08-21"); !later || !defined {
		t.Error("two timestamps compare as text")
	}
	if !jsAtMost(nil, 0.0) || jsAtMost("x", 1.0) {
		t.Error("null is zero, and a word is no number")
	}
}
