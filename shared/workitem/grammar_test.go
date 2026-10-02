package workitem

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The cases are the Node engine's own, from test/items/work-item.test.mjs
// and vocabulary.test.mjs at missingbulb/Claudinite@057841ac.

func TestTitleRoundTrips(t *testing.T) {
	if got := (Title{Pack: "acme-pack-b", Task: "acme-task-c"}).String(); got != "[claudinite-work] acme-pack-b/acme-task-c" {
		t.Errorf("%q", got)
	}
	got, ok := ParseTitle("[claudinite-work] acme-pack-c/acme-task-k member-repo-x")
	if !ok || got != (Title{"acme-pack-c", "acme-task-k", "member-repo-x"}) {
		t.Errorf("%+v %v", got, ok)
	}
	if _, ok := ParseTitle("[claudinite-task] acme-pack-b/acme-task-c d2026-08-14"); ok {
		t.Error("the slot mechanism's title parsed")
	}
	// A title filed before a pack's rename names the pack's id today.
	if got, _ := ParseTitle("[claudinite-work] static-website/x"); got.Pack != "public-website" {
		t.Errorf("renamed pack: %+v", got)
	}
}

func TestBodyRoundTrips(t *testing.T) {
	body := Body(BodySpec{TaskPath: "packs/acme-pack-b/tasks/acme-task-c/task.md", NotBefore: "2026-08-15T02:00:00.000Z",
		BlockedBy: []int{812, 813}, Context: []string{"only the mount", "nothing else"}})
	f := ParseBody(body)
	if f.TaskPath != "packs/acme-pack-b/tasks/acme-task-c/task.md" || f.NotBefore != "2026-08-15T02:00:00.000Z" || !reflect.DeepEqual(f.BlockedBy, []int{812, 813}) {
		t.Errorf("%+v", f)
	}
	if !strings.Contains(body, "### Context\n- only the mount\n- nothing else") {
		t.Errorf("%q", body)
	}
	raw, _ := json.Marshal(ParseFields(Body(BodySpec{TaskPath: "packs/x/tasks/y/task.md"})))
	want := `{"taskPath":"packs/x/tasks/y/task.md","notBefore":null,"blockedBy":[],"request":null,"model":null,"merge":null,"endsWhen":null,"targetBranch":null,"targetPr":null,"supersedes":[],"woken":null}`
	if string(raw) != want {
		t.Errorf("absence:\n%s\n%s", raw, want)
	}
}

func TestWokenAndFacts(t *testing.T) {
	bare := Body(BodySpec{TaskPath: "packs/x/tasks/y/task.md", Context: []string{"scope"}})
	once := WithWoken(bare, "2026-09-05T11:00:00Z")
	twice := WithWoken(once, "2026-09-06T00:00:00Z")
	if ParseBody(twice).Woken != "2026-09-06T00:00:00Z" || strings.Count(twice, "\nWoken:") != 1 || !strings.Contains(once, "### Context\n- scope") {
		t.Errorf("%q", twice)
	}
	facts := func(title, body string, labels ...string) bool {
		return ItemFacts(Issue{Number: 7, Title: title, Body: body, Labels: labels}).IsWoken
	}
	for _, c := range []struct {
		title, body string
		labels      []string
		want        bool
	}{
		{"[claudinite-work] x/y", bare, []string{OriginPlanned}, false},
		{"[claudinite-work] x/y", bare, nil, false},
		{"[claudinite-work] x/y", once, nil, true},
		{"[claudinite-work] x/y", bare, []string{OriginAdHoc}, true},
		{"[claudinite-work] x/y #12", bare, nil, true},
		{"Verify in production: a thing", bare, nil, true},
	} {
		if got := facts(c.title, c.body, c.labels...); got != c.want {
			t.Errorf("%q %v: woken %v", c.title, c.labels, got)
		}
	}
}

func TestNotBeforeEndsWhenTarget(t *testing.T) {
	fresh := Body(BodySpec{TaskPath: "p/t/task.md", Context: []string{"scope"}})
	stamped := WithNotBefore(WithNotBefore(fresh, "2026-08-15T04:00:00.000Z"), "2026-08-16T04:00:00.000Z")
	if ParseBody(stamped).NotBefore != "2026-08-16T04:00:00.000Z" || strings.Count(stamped, "Not-before:") != 1 {
		t.Errorf("%q", stamped)
	}
	if ParseBody(WithNotBefore(stamped, "")).NotBefore != "" {
		t.Error("not cleared")
	}
	ran := WithSection(fresh, DeliveredHeading, []string{"PR: #133 (open)"})
	ends := WithEndsWhen(WithEndsWhen(ran, 133), 140)
	if ParseBody(ends).EndsWhen != 140 || strings.Count(ends, "Ends-when:") != 1 || strings.Split(ends, "\n")[0] != "p/t/task.md" {
		t.Errorf("%q", ends)
	}
	for body, want := range map[string]int{"p/t.md\n\nEnds-when: #133 closed\n": 133, "p/t.md\n\nEnds-when: #133 merged\n": 0, "p/t.md\n\nEnds-when: whenever\n": 0} {
		if got := ParseBody(body).EndsWhen; got != want {
			t.Errorf("%q: %d", body, got)
		}
	}
	amend := WithTarget(ran, Target{Mode: "amend", Branch: "claudinite/p/t/2026-09-04-ab12", PR: 41})
	if f := ParseBody(amend); f.TargetBranch != "claudinite/p/t/2026-09-04-ab12" || f.TargetPR != 41 {
		t.Errorf("%+v", f)
	}
	re := WithTarget(amend, Target{Mode: "fresh", Branch: "claudinite/p/t/2026-09-05-cd34", PR: 0, Supersedes: []int{41, 40}})
	if f := ParseBody(re); f.TargetPR != 0 || !reflect.DeepEqual(f.Supersedes, []int{41, 40}) || strings.Count(re, "Target-branch:") != 1 {
		t.Errorf("%q", re)
	}
	if WithTarget(ran, Target{Mode: "none"}) != ran {
		t.Error("a body with no target changed")
	}
	marked := "Please do the thing.\n\n<!-- claudinite-item -->\npacks/p/tasks/a/task.md\n\nRequest: #7\n<!-- /claudinite-item -->\n"
	m := WithTarget(marked, Target{Mode: "fresh", Branch: "b", Supersedes: []int{2}})
	if !strings.HasPrefix(m, "Please do the thing.\n") || ParseBody(m).Request != 7 || ParseBody(m).TargetBranch != "b" {
		t.Errorf("%q", m)
	}
}

func TestSectionReplacedInPlace(t *testing.T) {
	born := Body(BodySpec{TaskPath: "packs/p/tasks/t/task.md", Context: []string{"born blocked"}})
	handed := WithSection(born, "Context", []string{"Issues to triage: #1, #2."})
	delivered := WithSection(WithSection(handed, "Context", []string{"#3"}), "Delivered by code-work", []string{"a branch"})
	again := WithSection(delivered, "Context", []string{"#4"})
	if strings.Count(again, "### Context") != 1 || strings.Contains(again, "born blocked") || strings.Index(again, "### Context") > strings.Index(again, "### Delivered") || !strings.Contains(again, "- a branch") {
		t.Errorf("%q", again)
	}
}

func TestStatusDecode(t *testing.T) {
	for legacy, want := range map[string]string{LegacyBlocked: StatusBlocked, LegacyReady: StatusReady, LegacyExecuting: StatusRunningExecutor,
		LegacyAgent: StatusRunningAgent, LegacyTaskDone: StatusDone, OutcomeDone: StatusDone, LegacyTaskObsolete: StatusRejected, OutcomeObsolete: StatusRejected} {
		if got := StatusOf([]string{legacy}); got != want {
			t.Errorf("%s: %s", legacy, got)
		}
	}
	if got := StatusOf([]string{NeedsHuman, "task:needs-human-approval"}); got != StatusNeedsHumanApprove {
		t.Errorf("legacy pair: %s", got)
	}
	if got := StatusOf([]string{NeedsHuman}); got != StatusNeedsHumanFailure {
		t.Errorf("bare park: %s", got)
	}
	if got := StatusOf([]string{StatusReady, StatusNeedsHumanAction}); got != StatusNeedsHumanAction {
		t.Errorf("park precedence: %s", got)
	}
	if got := StatusesOn([]string{LegacyReady, StatusReady}); len(got) != 1 {
		t.Errorf("one status: %v", got)
	}
	if StatusOf([]string{OriginSchedule, "bug"}) != "" {
		t.Error("an unknown label decoded")
	}
	if (Issue{Labels: []string{OriginSchedule}}).Origin() != "" || (Issue{Labels: []string{OriginManual}}).Origin() != OriginManual {
		t.Error("origin")
	}
	if !reflect.DeepEqual(SpellingsOf(StatusReady), []string{StatusReady, LegacyReady}) {
		t.Errorf("%v", SpellingsOf(StatusReady))
	}
	if TriageLabelFor("urgent") != StatusNeedsHumanFailure || TriageLabelFor("action") != StatusNeedsHumanAction {
		t.Error("triage")
	}
	for labels, want := range map[string]string{StatusDone: "done", OutcomeDone: "done", OutcomeDelivered: "delivered", StatusRejected: "obsolete", OutcomeObsolete: "obsolete", "": ""} {
		if got := (Issue{Labels: strings.Fields(labels)}).Outcome(); got != want {
			t.Errorf("%q: %q", labels, got)
		}
	}
}

func TestTaskIDFromPath(t *testing.T) {
	for path, want := range map[string]string{
		"packs/p/tasks/t/task.md":                                              "p/t",
		".claudinite/shared/packs/p/tasks/t/task.md":                           "p/t",
		"engine/scheduler/queue/tasks/implement-request/task.md":               "engine/implement-request",
		"packs/claudinite-tasks/queue/tasks/implement-request/task.md":         "engine/implement-request",
		"packs/claudinite-tasks/public/implement-request.md":                   "engine/implement-request",
		".claudinite/local/packs/claudinite-tasks/public/implement-request.md": "engine/implement-request",
	} {
		got, ok := TaskIDFromPath(path)
		if !ok || got.ID() != want {
			t.Errorf("%s: %+v", path, got)
		}
	}
	if _, ok := TaskIDFromPath("docs/x.md"); ok {
		t.Error("a doc path named a task")
	}
}

func TestPolicyFieldAndRequestFields(t *testing.T) {
	for raw, want := range map[string]string{"yes": "if-narrow", "Anything": "anything", "doc-changes ; reject:x": "doc-changes;reject:x",
		"under:a/b && doc-changes": "under:a/b&&doc-changes", "reject:x": "", "nothing": "", "under:../x": "", "Bad Name": ""} {
		if got := PolicyFieldValue(raw); got != want {
			t.Errorf("%q: %q want %q", raw, got, want)
		}
	}
	body := "Task: p/t\nModel: sonnet\nAutomerge: doc-changes\nBlocked-by: #3\n\nPlease."
	if r := ParseRequestFields(body, false); r.Task != "" || !r.Ungated || !reflect.DeepEqual(r.BlockedBy, []int{3}) {
		t.Errorf("ungated: %+v", r)
	}
	if r := ParseRequestFields(body, true); r.Task != "p/t" || r.Model != "sonnet" || r.Merge != "doc-changes" || r.Ungated {
		t.Errorf("gated: %+v", r)
	}
}

func TestLastVerdict(t *testing.T) {
	body := WithSection("task/path\n", LastVerdictHead, LastVerdictLines("2026-08-16T05:00:00Z", "quiet — nothing moved", "2026-08-17T04:00:00Z"))
	v, ok := ParseLastVerdict(body)
	if !ok || v.Reason != "quiet — nothing moved" || v.Until != "2026-08-17T04:00:00Z" {
		t.Errorf("%+v", v)
	}
	if _, ok := ParseLastVerdict("task/path\n"); ok {
		t.Error("never rolled")
	}
}

func TestQueueItemAndTrailer(t *testing.T) {
	if !(Issue{Title: "[claudinite-work] p/t"}).IsQueueItem() || (Issue{Title: "x", Labels: []string{OriginAdHoc}}).IsQueueItem() ||
		!(Issue{Title: "x", Labels: []string{OriginAdHoc, StatusReady}}).IsQueueItem() || !(Issue{Title: "x", Body: "a\n<!-- claudinite-item -->\np\n<!-- /claudinite-item -->"}).IsQueueItem() {
		t.Error("membership")
	}
	msg := WithTaskTrailer("Fold usage\n", "p/t")
	if msg != "Fold usage\n\nClaudinite-Task: p/t\n" || WithTaskTrailer(msg, "p/t") != msg || TaskFromMessage(msg) != "p/t" || WithTaskTrailer("m", "") != "m" {
		t.Errorf("%q", msg)
	}
}

func TestLabelListDecodesBothShapes(t *testing.T) {
	var i Issue
	if err := json.Unmarshal([]byte(`{"labels":["task:ready",{"name":"origin:schedule"}]}`), &i); err != nil || !reflect.DeepEqual([]string(i.Labels), []string{"task:ready", "origin:schedule"}) {
		t.Errorf("%v %v", i.Labels, err)
	}
}
