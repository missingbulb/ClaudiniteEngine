package precondition

import (
	"strings"
	"testing"
	"time"
)

func mark(s string) *UsageFold { return &UsageFold{RunsFoldedThrough: &s} }

// The fold runs when the machinery has moved since the last fold, and the
// only free evidence of that is the file's own watermark.
func TestRunsSinceFoldReadsTheFilesOwnWatermark(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		fold   *UsageFold
		holds  bool
		reason string
	}{
		{"never folded", &UsageFold{}, true, "nothing has been folded yet — every run this repo has made is uncounted"},
		{"no reading", nil, true, "nothing has been folded yet"},
		{"behind the UTC day", mark("2026-09-14T17:10:00Z"), true,
			"runs are folded through 2026-09-14T17:10:00Z, before this UTC day opened at 2026-09-15T00:00:00.000Z: the machinery has run since"},
		{"inside the UTC day", mark("2026-09-15T06:00:00Z"), false,
			"runs are folded through 2026-09-15T06:00:00Z, inside the UTC day that opened at 2026-09-15T00:00:00.000Z: nothing has run since the last fold"},
		{"exactly the day's opening", mark("2026-09-15T00:00:00.000Z"), false, "inside the UTC day"},
		{"a mark that is not text", &UsageFold{RunsFoldedThrough: strp("5"), NotText: true}, false, "folded through 5, inside"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := runsSinceFold(Signals{UsageFold: c.fold}, Opts{Now: &now})
			if o.Holds != c.holds || !strings.Contains(o.Reason, c.reason) || o.Error != "" {
				t.Fatalf("%+v", o)
			}
		})
	}
	if o := runsSinceFold(Signals{UsageFold: mark("2026-09-14T17:10:00Z")}, Opts{}); o.Error == "" {
		t.Fatalf("a verdict with no now: %+v", o)
	}
	if !EngineJudged("runs-since-fold") {
		t.Fatal("the runner would ask a preconditions.mjs for an engine term")
	}
}

func strp(s string) *string { return &s }
