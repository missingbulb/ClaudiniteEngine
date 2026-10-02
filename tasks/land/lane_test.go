package land

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDeliveryForOnlyAnExplicitTrueWithholdsThePR(t *testing.T) {
	cases := []struct {
		settings map[string]any
		want     string
	}{
		{map[string]any{"dailyClaudiniteUpdatesRequirePrReview": true}, Review},
		{map[string]any{"dailyClaudiniteUpdatesRequirePrReview": false}, AutoMerge},
		{map[string]any{"packs": []any{"acme-pack"}}, AutoMerge},
		{nil, AutoMerge},
		{map[string]any{"maintenance": map[string]any{"delivery": "review"}}, Review},
		{map[string]any{"maintenance": map[string]any{"delivery": "pr"}}, Review},
		{map[string]any{"maintenance": map[string]any{"delivery": "auto-merge"}}, AutoMerge},
		{map[string]any{"maintenance": map[string]any{"delivery": "nonsense"}}, AutoMerge},
		{map[string]any{"dailyClaudiniteUpdatesRequirePrReview": false, "maintenance": map[string]any{"delivery": "review"}}, AutoMerge},
	}
	for _, c := range cases {
		if got := DeliveryFor(c.settings); got != c.want {
			t.Errorf("%v: %s, want %s", c.settings, got, c.want)
		}
	}
}

func TestWorkflowTriggersReadsEveryShape(t *testing.T) {
	cases := map[string][]string{
		"name: Tests\non:\n  workflow_dispatch:\n  pull_request:\n    branches: [main]\n  push:\n    branches: [main, \"claude/**\"]\njobs:\n  test:\n    runs-on: ubuntu-latest": {"workflow_dispatch", "pull_request", "push"},
		"name: CI\non:\n  pull_request:\n  push:\n    branches: [main]\njobs: {}\n":                                                                                          {"pull_request", "push"},
		"on: workflow_dispatch\njobs: {}\n":     {"workflow_dispatch"},
		"on: [pull_request, push]\njobs: {}\n":  {"pull_request", "push"},
		"'on':\n  push:\njobs: {}\n":            {"push"},
		"name: fragment\njobs: {}\n":            nil,
		"on:\n  schedule:\n    - cron: \"24 * * * *\"\n  workflow_dispatch:\n    inputs:\n      overrides:\n        required: false\njobs: {}": {"schedule", "workflow_dispatch"},
	}
	for yaml, want := range cases {
		if got := WorkflowTriggers(yaml); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", yaml, got, want)
		}
	}
}

func TestCIDispatchPlanDispatchesOnlyPRTriggeredDispatchableWorkflows(t *testing.T) {
	got := CIDispatchPlan([]WorkflowFile{
		{"test.yml", "on:\n  workflow_dispatch:\n  pull_request:\n    branches: [main]\njobs: {}\n"},
		{"release.yml", "on:\n  workflow_dispatch:\njobs: {}\n"},
		{"scheduler.yml", "on:\n  schedule:\n    - cron: \"0 * * * *\"\njobs: {}\n"},
		{"ci.yml", "on:\n  pull_request:\n  push:\n    branches: [main]\njobs: {}\n"},
	})
	if !reflect.DeepEqual(got.Dispatch, []string{"test.yml"}) || !reflect.DeepEqual(got.Missing, []string{"ci.yml"}) {
		t.Errorf("%+v", got)
	}
}

func TestActionByMemberShape(t *testing.T) {
	cases := []struct {
		delivery string
		ci       bool
		gate     Gate
		want     Action
	}{
		{AutoMerge, false, GateUnknown, ActMerge},
		{AutoMerge, false, GatePresent, ActMerge},
		{AutoMerge, true, GatePresent, ActArm},
		{AutoMerge, true, GateUnknown, ActArm},
		{AutoMerge, true, GateAbsent, ActLand},
		{Review, false, GateUnknown, ActNone},
		{Review, true, GateAbsent, ActNone},
	}
	for _, c := range cases {
		if got := DeliveryAction(c.delivery, c.ci, c.gate); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
}

func TestClassifyMergeGateNeverReadsUnknownAsAbsent(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		protected *bool
		rules     []string
		rulesOK   bool
		want      Gate
	}{
		{&yes, nil, true, GatePresent},
		{&no, []string{"required_status_checks"}, true, GatePresent},
		{&no, []string{"pull_request"}, true, GatePresent},
		{&no, nil, true, GateAbsent},
		{&no, []string{"deletion", "non_fast_forward"}, true, GateAbsent},
		{nil, nil, true, GateUnknown},
		{&no, nil, false, GateUnknown},
	}
	for _, c := range cases {
		if got := ClassifyMergeGate(c.protected, c.rules, c.rulesOK); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
}

func done(conclusion string, name ...string) Run {
	n := "CI"
	if len(name) > 0 {
		n = name[0]
	}
	return Run{Name: n, Status: "completed", Conclusion: conclusion}
}

func running(name string) Run { return Run{Name: name, Status: "in_progress"} }

func TestPullDisposition(t *testing.T) {
	cases := []struct {
		delivery string
		runs     []Run
		want     Disposition
	}{
		{AutoMerge, []Run{done("success"), done("action_required")}, DispMerge},
		{AutoMerge, []Run{done("success")}, DispMerge},
		{AutoMerge, []Run{done("success"), done("action_required"), done("failure")}, DispClose},
		{AutoMerge, []Run{done("timed_out")}, DispClose},
		{AutoMerge, []Run{done("cancelled")}, DispClose},
		{AutoMerge, []Run{done("startup_failure"), done("success")}, DispClose},
		{AutoMerge, []Run{done("success"), running("CI")}, DispWait},
		{AutoMerge, []Run{{Name: "CI", Status: "queued"}}, DispWait},
		{AutoMerge, nil, DispClose},
		{AutoMerge, []Run{done("action_required")}, DispClose},
		{AutoMerge, []Run{done("skipped")}, DispClose},
		{Review, []Run{done("success")}, DispKeep},
		{Review, []Run{done("failure")}, DispKeep},
		{Review, nil, DispKeep},
	}
	for _, c := range cases {
		if got := PullDisposition(c.delivery, c.runs); got != c.want {
			t.Errorf("%s %+v: %s, want %s", c.delivery, c.runs, got, c.want)
		}
	}
}

func TestLandAttemptKeepsTwoBoundsAndNeverCloses(t *testing.T) {
	inFlight := []Run{done("success"), running("UI requirements")}
	held := done("action_required")
	cases := []struct {
		name     string
		delivery string
		runs     []Run
		expected int
		elapsed  time.Duration
		want     Attempt
	}{
		{"gated shape lands", AutoMerge, []Run{done("success"), held}, 0, 0, AttemptMerge},
		{"in flight polls", AutoMerge, inFlight, 0, 0, AttemptPoll},
		{"in flight polls to its bound", AutoMerge, inFlight, 0, InflightTimeout - time.Millisecond, AttemptPoll},
		{"in flight gives up at its bound", AutoMerge, inFlight, 0, InflightTimeout, AttemptGiveUp},
		{"slow check outlives the visibility bound", AutoMerge, inFlight, 2, 183 * time.Second, AttemptPoll},
		{"slow check lands", AutoMerge, []Run{done("success"), done("success", "UI requirements")}, 2, 209 * time.Second, AttemptMerge},
		{"nothing visible gives up at the short bound", AutoMerge, nil, 2, Timeout, AttemptGiveUp},
		{"red stands", AutoMerge, []Run{done("failure")}, 0, 0, AttemptGiveUp},
		{"timed out stands", AutoMerge, []Run{done("success"), done("timed_out")}, 0, 0, AttemptGiveUp},
		{"empty stands", AutoMerge, nil, 0, 0, AttemptGiveUp},
		{"held alone stands", AutoMerge, []Run{held}, 0, 0, AttemptGiveUp},
		{"waits for what it dispatched", AutoMerge, nil, 1, 0, AttemptPoll},
		{"a held run is not a dispatched one", AutoMerge, []Run{held}, 1, 0, AttemptPoll},
		{"held twice", AutoMerge, []Run{held}, 2, 0, AttemptPoll},
		{"dispatched never registers", AutoMerge, nil, 1, Timeout, AttemptGiveUp},
		{"dispatched and green", AutoMerge, []Run{done("success")}, 1, 0, AttemptMerge},
		{"held beside green lands", AutoMerge, []Run{held, done("success")}, 1, 40 * time.Second, AttemptMerge},
		{"review member", Review, []Run{done("success")}, 0, 0, AttemptGiveUp},
	}
	for _, c := range cases {
		if got := LandAttempt(c.delivery, c.runs, c.expected, c.elapsed); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestReasonsNameTheShapeAndTheClock(t *testing.T) {
	if r := MergeReason([]Run{done("success"), done("action_required")}); !strings.Contains(r, "action_required") {
		t.Error(r)
	}
	if r := MergeReason([]Run{done("success")}); strings.Contains(r, "Allow auto-merge") || !strings.Contains(r, "nothing on the base branch queues") {
		t.Error(r)
	}
	if s := FailureSummary([]Run{done("failure", "verify"), done("success")}); !strings.Contains(s, "verify failure") {
		t.Error(s)
	}
	if s := FailureSummary([]Run{done("action_required")}); !strings.Contains(s, "no successful run") {
		t.Error(s)
	}
	s := FailureSummary([]Run{done("success"), running("UI requirements")})
	if !strings.Contains(s, "still running") || !strings.Contains(s, "UI requirements") || strings.Contains(s, "no successful run") {
		t.Error(s)
	}
	if s := FailureSummary([]Run{{Name: "CI", Status: "queued"}, done("failure", "verify")}); !strings.Contains(s, "verify failure") {
		t.Error(s)
	}
}

// fakeAPI is a repository the lane delivers into.
type fakeAPI struct {
	files      []WorkflowFile
	filesErr   error
	dispatchOK map[string]bool
	protected  *bool
	rules      []string
	rulesErr   error
	runs       [][]Run // successive reads
	armErr     error
	mergeErr   error

	dispatched []string
	merged     []Merge
	armed      []string
	deleted    []string
	reads      int
}

func (f *fakeAPI) WorkflowFiles(ref string) ([]WorkflowFile, error) { return f.files, f.filesErr }
func (f *fakeAPI) DispatchWorkflow(name, ref string) error {
	if !f.dispatchOK[name] {
		return &StatusError{Status: 403}
	}
	f.dispatched = append(f.dispatched, name+"@"+ref)
	return nil
}
func (f *fakeAPI) BranchProtected(base string) (*bool, error) { return f.protected, nil }
func (f *fakeAPI) BranchRules(base string) ([]string, error)  { return f.rules, f.rulesErr }
func (f *fakeAPI) RunsForSHA(sha string) ([]Run, error) {
	i := f.reads
	if i >= len(f.runs) {
		i = len(f.runs) - 1
	}
	f.reads++
	if i < 0 {
		return nil, nil
	}
	return f.runs[i], nil
}
func (f *fakeAPI) MergePull(m Merge) error {
	if f.mergeErr != nil {
		return f.mergeErr
	}
	f.merged = append(f.merged, m)
	return nil
}
func (f *fakeAPI) EnableAutoMerge(nodeID string) error {
	if f.armErr != nil {
		return f.armErr
	}
	f.armed = append(f.armed, nodeID)
	return nil
}
func (f *fakeAPI) DeleteBranch(ref string) error { f.deleted = append(f.deleted, ref); return nil }

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time        { return c.t }
func (c *fakeClock) Sleep(d time.Duration) { c.t = c.t.Add(d) }

const prCI = "on:\n  workflow_dispatch:\n  pull_request:\njobs: {}\n"

func lane(api *fakeAPI) (Lane, *[]string) {
	var logs []string
	c := &fakeClock{t: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	return Lane{API: api, Now: c.Now, Sleep: c.Sleep, Log: func(s string) { logs = append(logs, s) }}, &logs
}

var pr = PR{Number: 7, NodeID: "PR_7", HeadRef: "claudinite/acme-pack/acme-task/2026-10-01-abc", HeadSHA: "abc123"}

func TestDeliverMergesAtThePinnedShaWhenNoPRCIExists(t *testing.T) {
	api := &fakeAPI{files: []WorkflowFile{{"release.yml", "on:\n  workflow_dispatch:\n"}}}
	l, _ := lane(api)
	got := l.Deliver(pr, "main", AutoMerge, "acme-pack/acme-task")
	if !got.Merged || got.Action != ActMerge {
		t.Fatalf("%+v", got)
	}
	want := Merge{Number: 7, SHA: "abc123", Message: "Claudinite-Task: acme-pack/acme-task"}
	if !reflect.DeepEqual(api.merged, []Merge{want}) {
		t.Errorf("%+v", api.merged)
	}
	if !reflect.DeepEqual(api.deleted, []string{pr.HeadRef}) {
		t.Errorf("branch %v", api.deleted)
	}
}

func TestDeliverLeavesAReviewMembersPRAfterStartingItsChecks(t *testing.T) {
	api := &fakeAPI{files: []WorkflowFile{{"test.yml", prCI}}, dispatchOK: map[string]bool{"test.yml": true}}
	l, _ := lane(api)
	got := l.Deliver(pr, "main", Review, "acme-pack/acme-task")
	if got.Merged || got.Action != ActNone || len(api.merged) != 0 || len(api.armed) != 0 {
		t.Fatalf("%+v %+v", got, api)
	}
	if !reflect.DeepEqual(api.dispatched, []string{"test.yml@" + pr.HeadRef}) {
		t.Errorf("%v", api.dispatched)
	}
}

func TestDeliverSkipsTheDoomedArmOnAnUngatedBaseAndLandsOnItsOwnEvidence(t *testing.T) {
	no := false
	api := &fakeAPI{
		files: []WorkflowFile{{"test.yml", prCI}}, dispatchOK: map[string]bool{"test.yml": true},
		protected: &no, rules: []string{"deletion"},
		runs: [][]Run{nil, {running("test")}, {done("success", "test")}},
	}
	l, _ := lane(api)
	got := l.Deliver(pr, "main", AutoMerge, "acme-pack/acme-task")
	if !got.Merged || got.Action != ActLand || len(api.armed) != 0 || api.reads != 3 {
		t.Fatalf("%+v reads %d armed %v", got, api.reads, api.armed)
	}
}

func TestDeliverArmsBehindAGateAndPollsOnlyWhenTheArmFails(t *testing.T) {
	yes := true
	api := &fakeAPI{files: []WorkflowFile{{"test.yml", prCI}}, dispatchOK: map[string]bool{"test.yml": true}, protected: &yes}
	l, _ := lane(api)
	got := l.Deliver(pr, "main", AutoMerge, "")
	if got.Merged || got.Action != ActArm || !reflect.DeepEqual(api.armed, []string{"PR_7"}) || api.reads != 0 {
		t.Fatalf("%+v %+v", got, api)
	}

	api = &fakeAPI{files: []WorkflowFile{{"test.yml", prCI}}, dispatchOK: map[string]bool{"test.yml": true}, protected: &yes,
		armErr: errors.New("Pull request is in clean status"), runs: [][]Run{{done("success", "test"), done("action_required", "test")}}}
	l, logs := lane(api)
	got = l.Deliver(pr, "main", AutoMerge, "")
	if !got.Merged || api.merged[0].Message != "" {
		t.Fatalf("%+v %+v", got, api.merged)
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "Allow auto-merge") || !strings.Contains(strings.Join(*logs, "\n"), "action_required") {
		t.Errorf("%v", *logs)
	}
}

func TestDeliverTreatsAnUnreadableTreeAsCIAndADeniedDispatchAsNothingToWaitFor(t *testing.T) {
	api := &fakeAPI{filesErr: errors.New("500"), protected: nil}
	l, _ := lane(api)
	if got := l.Deliver(pr, "main", AutoMerge, ""); got.Action != ActArm {
		t.Errorf("unreadable tree merged blind: %+v", got)
	}
	no := false
	api = &fakeAPI{files: []WorkflowFile{{"test.yml", prCI}}, dispatchOK: map[string]bool{}, protected: &no, runs: [][]Run{nil}}
	l, logs := lane(api)
	got := l.Deliver(pr, "main", AutoMerge, "")
	if got.Merged || api.reads != 1 {
		t.Errorf("%+v reads %d", got, api.reads)
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "actions: write") {
		t.Errorf("%v", *logs)
	}
}

func TestLandNowLeavesThePRWhenTheMergeIsRefused(t *testing.T) {
	no := false
	api := &fakeAPI{files: []WorkflowFile{{"test.yml", prCI}}, dispatchOK: map[string]bool{"test.yml": true}, protected: &no,
		runs: [][]Run{{done("success", "test")}}, mergeErr: &StatusError{Status: 405, Message: "Head branch was modified"}}
	l, logs := lane(api)
	if got := l.Deliver(pr, "main", AutoMerge, ""); got.Merged {
		t.Fatalf("%+v", got)
	}
	if len(api.deleted) != 0 || !strings.Contains(strings.Join(*logs, "\n"), "Head branch was modified") {
		t.Errorf("%v %v", api.deleted, *logs)
	}
}

func TestMergeIsThePinnedShaSquashAndTidiesTheBranch(t *testing.T) {
	api := &fakeAPI{}
	l, _ := lane(api)
	if err := l.Merge(pr, "Update the engine to 1.4.0", ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(api.merged, []Merge{{Number: 7, SHA: "abc123", Title: "Update the engine to 1.4.0"}}) || len(api.deleted) != 1 {
		t.Errorf("%+v %v", api.merged, api.deleted)
	}
}
