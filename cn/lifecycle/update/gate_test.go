package update

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// testsRun is the member's own Tests workflow on a head.
func testsRun(id int64, sha, status, conclusion string) githubapi.Run {
	return githubapi.Run{ID: id, Name: "Tests", HeadSHA: sha, Event: "pull_request", Status: status, Conclusion: conclusion, CreatedAt: "2026-10-01T00:00:00Z"}
}

// An update PR whose claudinite-ci passed is not landed while another
// workflow on its head failed, and the verdict names that workflow; one
// still running leaves the PR for its verdict.
func TestAnUpdatePRWithARedWorkflowBesideGreenCIIsNotLanded(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha := w.openUpdatePR(t, 4, v2, "success")
	w.hub.runs[sha] = append(w.hub.runs[sha], testsRun(90, sha, "completed", "failure"))
	r, err := EngineRun(w.deps(t), Options{})
	if err != nil || len(w.hub.called("merge")) != 0 {
		t.Fatalf("landed over a red Tests run: %+v %v", r, err)
	}
	if r.Verdict != "skipped: #4 for "+v2+" is open and its CI concluded failure in Tests" || r.PRPending {
		t.Errorf("%+v", r)
	}

	w = newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha = w.openUpdatePR(t, 4, v2, "success")
	w.hub.runs[sha] = append(w.hub.runs[sha], testsRun(90, sha, "in_progress", ""))
	r, err = EngineRun(w.deps(t), Options{})
	if err != nil || len(w.hub.called("merge")) != 0 {
		t.Fatalf("landed over a running Tests run: %+v %v", r, err)
	}
	if r.Verdict != "skipped: #4 for "+v2+" is open and its CI is in_progress" || !r.PRPending {
		t.Errorf("%+v", r)
	}
}

// The run that opens an update PR lands it on every workflow's runs on
// its head, not claudinite-ci's alone.
func TestAnUpdatePRIsLandedInItsRunOnlyOnEveryWorkflowGreen(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.hub.approved = "success"
	w.hub.onOpen = []githubapi.Run{testsRun(90, "", "completed", "failure")}
	r, err := EngineRun(w.deps(t), Options{})
	if err != nil || r.Verdict != "opened #1 for "+v2 || r.PRPending || len(w.hub.called("merge")) != 0 {
		t.Fatalf("%+v %v\n%s", r, err, w.out)
	}
	if !strings.Contains(w.out.String(), "Tests failure") {
		t.Errorf("the failing workflow is not named:\n%s", w.out)
	}

	w = newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.hub.approved = "success"
	w.hub.onOpen = []githubapi.Run{testsRun(90, "", "in_progress", "")}
	r, err = EngineRun(w.deps(t), Options{})
	if err != nil || r.Verdict != "opened #1 for "+v2 || !r.PRPending || len(w.hub.called("merge")) != 0 {
		t.Fatalf("%+v %v\n%s", r, err, w.out)
	}

	w = newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.hub.approved = "success"
	w.hub.onOpen = []githubapi.Run{testsRun(90, "", "completed", "success")}
	if r, err = EngineRun(w.deps(t), Options{}); err != nil || r.Verdict != "landed "+v2 {
		t.Fatalf("every workflow green: %+v %v\n%s", r, err, w.out)
	}
}

// landJob is d as the land job of a claudinite-ci run dispatched on sha
// runs it: that run is on the head, still in progress, and is this cn's.
func landJob(hub *fakeGitHub, d Deps, sha string) Deps {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.runs[sha] = append(hub.runs[sha], githubapi.Run{ID: 999, Name: "claudinite-ci", HeadSHA: sha, Event: "workflow_dispatch",
		Status: "in_progress", CreatedAt: "2026-10-02T00:00:00Z"})
	d.SelfRun = 999
	return d
}

// cn update land, run by the dispatched claudinite-ci's land job, refuses
// while another workflow on the head is red, naming it, and waits for
// one still running; its own run counts green.
func TestLandRefusesWhileAnotherWorkflowIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha := w.openUpdatePR(t, 4, v2, "success")
	w.hub.runs[sha] = append(w.hub.runs[sha], testsRun(90, sha, "completed", "failure"))
	v, err := Land(w.deps(t), 4, sha)
	if err != nil || v != "skipped: #4 not landed: failing CI: Tests failure" || len(w.hub.called("merge")) != 0 {
		t.Fatalf("%q %v", v, err)
	}

	w = newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha = w.openUpdatePR(t, 4, v2, "")
	w.hub.runs[sha] = []githubapi.Run{testsRun(90, sha, "in_progress", "")}
	v, err = Land(landJob(w.hub, w.deps(t), sha), 4, sha)
	if err != nil || v != "skipped: #4 not landed: still running at the landing bound: Tests in_progress — it lands next cycle" || len(w.hub.called("merge")) != 0 {
		t.Fatalf("Tests still running: %q %v", v, err)
	}

	w = newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha = w.openUpdatePR(t, 4, v2, "")
	w.hub.runs[sha] = []githubapi.Run{testsRun(90, sha, "completed", "success"),
		{ID: 91, Name: "Lint", HeadSHA: sha, Event: "pull_request", Status: "completed", Conclusion: "action_required"}}
	if v, err = Land(landJob(w.hub, w.deps(t), sha), 4, sha); err != nil || v != "landed "+v2 {
		t.Fatalf("its own run green, Tests green, Lint never approved: %q %v", v, err)
	}
}

// A workflow re-run on the head supersedes its earlier verdict: its
// newest run is the one that counts.
func TestANewerRunOfAWorkflowSupersedesItsOlderOne(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	sha := w.openUpdatePR(t, 4, v2, "success")
	old := testsRun(90, sha, "completed", "failure")
	again := testsRun(91, sha, "completed", "success")
	again.Event, again.CreatedAt = "workflow_dispatch", "2026-10-03T00:00:00Z"
	w.hub.runs[sha] = append(w.hub.runs[sha], old, again)
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "landed "+v2 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
}
