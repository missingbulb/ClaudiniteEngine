package sim

import (
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/land"
)

// A PR the simulated job token opens gets its pull_request run held; the
// lane approves it, lands on its verdict and dispatches only on the base.
func TestTheLaneApprovesTheRunASimulatedPROpensHeld(t *testing.T) {
	r := NewRepo()
	no := false
	r.Protected, r.HeldOnOpen, r.Approved = &no, []string{"claudinite-ci.yml"}, "success"
	r.Workflows = []land.WorkflowFile{{Name: "claudinite-ci.yml", Content: "on:\n  pull_request:\n  workflow_dispatch:\n"}}
	r.Dispatchable[land.CIWorkflow] = true
	p, _ := r.CreatePull("t", "b", "claudinite/acme-pack/acme-task/x", "main")
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	l := land.Lane{API: r, Now: func() time.Time { return now }, Sleep: func(d time.Duration) { now = now.Add(d) }, Log: func(string) {}}
	got := l.Deliver(land.PR{Number: p.Number, NodeID: p.NodeID, HeadRef: p.HeadRef, HeadSHA: p.HeadSHA}, "main", land.AutoMerge, "", nil)
	if !got.Merged {
		t.Fatalf("%+v %v", got, r.Log)
	}
	log := strings.Join(r.Log, "\n")
	if !strings.Contains(log, "approve 1") || strings.Contains(log, "dispatch claudinite-ci.yml claudinite/") || !strings.Contains(log, "dispatch claudinite-ci.yml main") {
		t.Errorf("%s", log)
	}
}
