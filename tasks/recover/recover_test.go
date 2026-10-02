package recover

import (
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/tasks/sim"
)

func TestAnAbsentOrUnreadableDepthIsTheFirstDeath(t *testing.T) {
	for raw, want := range map[string]int{"": 1, "  ": 1, "x": 1, "-4": 1, "0": 1, "1": 2, " 2 ": 3, "2.7": 3} {
		if got := NextDepth(raw); got != want {
			t.Errorf("%q: %d, want %d", raw, got, want)
		}
	}
}

func TestADeathWithinTheBoundDispatchesTheNextLink(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)))
	var sent []map[string]string
	var logs []string
	err := Continue(In{Issues: gh, Branch: "main", Depth: 3, RunURL: "https://run",
		Dispatch: func(inputs map[string]string) error { sent = append(sent, inputs); return nil },
		Log:      func(s string) { logs = append(logs, s) }})
	if err != nil || len(sent) != 1 || sent[0]["continuation_depth"] != "3" {
		t.Fatal(err, sent)
	}
	if len(gh.All()) != 0 || !strings.Contains(logs[0], "continuation 3 of 3") {
		t.Error(gh.All(), logs)
	}
}

func TestPastTheBoundTheChainStopsOnTheOneFailureIssue(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)))
	dispatched := 0
	in := In{Issues: gh, Branch: "main", Depth: 4, RunURL: "https://run",
		Dispatch: func(map[string]string) error { dispatched++; return nil }, Log: func(string) {}}
	for i := 0; i < 2; i++ {
		if err := Continue(in); err == nil || !strings.Contains(err.Error(), "died 3 times") {
			t.Fatal(err)
		}
	}
	all := gh.All()
	if dispatched != 0 || len(all) != 1 || all[0].Title != ChainFailureTitle || len(all[0].Comments) != 1 {
		t.Fatalf("%d %+v", dispatched, all)
	}
	if !strings.Contains(all[0].Body, "https://run") {
		t.Error(all[0].Body)
	}
}

func TestARefusedDispatchFailsTheJob(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(time.Now()))
	err := Continue(In{Issues: gh, Branch: "main", Depth: 1, Log: func(string) {},
		Dispatch: func(map[string]string) error { return errFake("403") }})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Error(err)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
