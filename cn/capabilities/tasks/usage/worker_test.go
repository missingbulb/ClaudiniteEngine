package usage

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestParseLogNameTakesTheCollisionSuffixTheIssueZeroFormAndThePRKey(t *testing.T) {
	n, ok := ParseLogName("2026-07-28T0940Z-2--issue-9--s1.jsonl")
	if !ok || n.Date != "2026-07-28" || n.Stamp != "2026-07-28T09:40:00Z" || n.Issue == nil || *n.Issue != 9 || n.PR != nil || n.SessionID != "s1" {
		t.Fatalf("%+v", n)
	}
	if n, _ := ParseLogName("2026-07-28T0940Z--issue-0--s1.jsonl"); n.Issue == nil || *n.Issue != 0 {
		t.Fatalf("%+v", n)
	}
	n, ok = ParseLogName("2026-07-28T0940Z--pr-1583--s1.jsonl")
	if !ok || n.Stamp != "2026-07-28T09:40:00Z" || n.Issue != nil || n.PR == nil || *n.PR != 1583 || n.SessionID != "s1" {
		t.Fatalf("%+v", n)
	}
	for _, name := range []string{"README.md", "notes.jsonl"} {
		if _, ok := ParseLogName(name); ok {
			t.Errorf("%s is not a capture", name)
		}
	}
}

func TestParseEntriesSkipsAPartialTrailingWrite(t *testing.T) {
	entries := ParseEntries("{\"type\":\"user\"}\nnot json\n\n{\"type\":\"assistant\"}\n")
	var types []string
	for _, e := range entries {
		types = append(types, field(e, "type").(string))
	}
	if !reflect.DeepEqual(types, []string{"user", "assistant"}) {
		t.Fatalf("%v", types)
	}
}

func TestParseCommitLogAttributesEachNumstatBlockToTheCommitThatOpenedIt(t *testing.T) {
	log := strings.Join([]string{
		"\u00012026-08-21T10:00:00+00:00",
		"10\t2\tsrc/a.mjs",
		"4\t0\tsrc/b.mjs",
		"\u00012026-08-21T18:30:00+00:00",
		"1\t1\tREADME.md",
		"-\t-\tlogo.png",
		"\u00012026-08-20T09:00:00+00:00",
		"\u00012026-08-20T11:00:00+00:00",
		"7\t3\tsrc/c.mjs",
	}, "\n")
	same(t, ParseCommitLog(log), `{"2026-08-21": {"commits": 2, "linesAdded": 15, "linesRemoved": 3},
		"2026-08-20": {"commits": 2, "linesAdded": 7, "linesRemoved": 3}}`)
	same(t, ParseCommitLog(""), `{}`)
}

func TestDayFieldsFromLeavesDaysTheHistoryCouldNotReachWithoutKeys(t *testing.T) {
	ladder := []string{"2026-08-18", "2026-08-19", "2026-08-20"}
	fields := DayFieldsFrom(
		&CommitSeries{CoveredFrom: "2026-08-19", Days: jsObj(t, `{"2026-08-20": {"commits": 2, "linesAdded": 9, "linesRemoved": 1}}`)},
		&ReleaseSeries{Days: jsObj(t, `{"2026-08-20": 1}`)}, ladder)
	if v := field(fields, "2026-08-18.commits"); v != nil {
		t.Errorf("before the history starts — unknown: %v", v)
	}
	if v := field(fields, "2026-08-18.releases"); v != 0.0 {
		t.Errorf("but the releases listing did reach it: %v", v)
	}
	same(t, field(fields, "2026-08-19"), `{"commits": 0, "linesAdded": 0, "linesRemoved": 0, "releases": 0}`)
	same(t, field(fields, "2026-08-20"), `{"commits": 2, "linesAdded": 9, "linesRemoved": 1, "releases": 1}`)
}

func TestDayFieldsFromWritesNothingWhenNeitherSourceCouldBeRead(t *testing.T) {
	same(t, DayFieldsFrom(nil, nil, []string{"2026-08-20"}), `{}`)
}

func TestDayLadderIsAUTCLadderEndingToday(t *testing.T) {
	if got := DayLadder("2026-08-21T11:00:00Z", 3); !reflect.DeepEqual(got, []string{"2026-08-19", "2026-08-20", "2026-08-21"}) {
		t.Fatalf("%v", got)
	}
}

// history answers the local reads with local and records every call; the
// fetch goes to the engine's, failing when fail is set.
func history(calls *[][]string, local func([]string) string, fail bool) History {
	return History{
		Local: func(args ...string) (string, error) {
			*calls = append(*calls, args)
			return local(args), nil
		},
		Remote: func(args ...string) (string, error) {
			*calls = append(*calls, args)
			if fail {
				return "", errors.New("the server will not deepen")
			}
			return "", nil
		},
	}
}

func TestDeepenHistoryFetchesTheWindowOnAShallowCheckout(t *testing.T) {
	var calls [][]string
	h := history(&calls, func(args []string) string {
		if args[0] == "rev-parse" {
			return "true\n"
		}
		return ""
	}, false)
	if got := DeepenHistory(h, "main", "2026-07-22T00:00:00Z"); got != "deepened" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(calls[1], []string{"fetch", "--quiet", "--shallow-since=2026-07-22T00:00:00Z", "origin", "main"}) {
		t.Fatalf("%v", calls)
	}
}

func TestDeepenHistoryLeavesACompleteCloneAlone(t *testing.T) {
	var calls [][]string
	h := history(&calls, func([]string) string { return "false\n" }, false)
	if got := DeepenHistory(h, "main", "2026-07-22T00:00:00Z"); got != "complete" {
		t.Fatal(got)
	}
	if len(calls) != 1 {
		t.Fatalf("it asks, and then does nothing at all: %v", calls)
	}
}

func TestADeepenThatCouldNotRunSaysSo(t *testing.T) {
	var calls [][]string
	h := history(&calls, func([]string) string { return "true\n" }, true)
	if got := DeepenHistory(h, "main", "2026-07-22T00:00:00Z"); got != "unchanged" {
		t.Fatal(got)
	}
}

func half(path, text string, moves ...Move) func() (Folded, error) {
	return func() (Folded, error) {
		return Folded{Files: []File{{path, text}}, Moves: moves, Summary: path + " folded"}, nil
	}
}

func unchangedHalf(summary string) func() (Folded, error) {
	return func() (Folded, error) { return Folded{Summary: summary}, nil }
}

type recorder struct{ calls []Change }

func (r *recorder) deliver(c Change) (Delivered, error) {
	r.calls = append(r.calls, c)
	return Delivered{Branch: "b", Number: 7}, nil
}

func TestDeliverFoldsPutsBothHalvesFilesAndBothMovesOnOnePullRequest(t *testing.T) {
	var r recorder
	_, err := DeliverFolds([]Half{
		{"sessions", half("a.json", "A", Move{"old-a", "a.json"})},
		{"machinery", half("b.json", "B", Move{"old-b", "b.json"})},
	}, r.deliver, func(string) {})
	if err != nil || len(r.calls) != 1 {
		t.Fatalf("%v %v", err, r.calls)
	}
	if !reflect.DeepEqual(r.calls[0].Files, []File{{"a.json", "A"}, {"b.json", "B"}}) ||
		!reflect.DeepEqual(r.calls[0].Moves, []Move{{"old-a", "a.json"}, {"old-b", "b.json"}}) {
		t.Fatalf("%+v", r.calls[0])
	}
}

func TestDeliverFoldsOpensNothingWhenNeitherHalfChangedAByte(t *testing.T) {
	var r recorder
	var lines []string
	if _, err := DeliverFolds([]Half{{"sessions", unchangedHalf("s")}, {"machinery", unchangedHalf("m")}}, r.deliver,
		func(l string) { lines = append(lines, l) }); err != nil || len(r.calls) != 0 {
		t.Fatalf("%v %v", err, r.calls)
	}
	if lines[len(lines)-1] != "every recompute is byte-identical - nothing to deliver" {
		t.Fatalf("%v", lines)
	}
}

func TestAHalfThatFailsCostsOnlyItsOwnFile(t *testing.T) {
	var r recorder
	var lines []string
	_, err := DeliverFolds([]Half{
		{"sessions", func() (Folded, error) { return Folded{}, errors.New("logs branch unreadable") }},
		{"machinery", half("b.json", "B")},
	}, r.deliver, func(l string) { lines = append(lines, l) })
	if err == nil || !regexp.MustCompile(`sessions.*logs branch unreadable`).MatchString(err.Error()) {
		t.Fatalf("%v", err)
	}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0].Files, []File{{"b.json", "B"}}) {
		t.Fatalf("%v", r.calls)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "sessions half failed") {
		t.Fatalf("%v", lines)
	}
}

func TestAHalfsReportRidesThePullRequestBody(t *testing.T) {
	var r recorder
	_, err := DeliverFolds([]Half{{"sessions", func() (Folded, error) {
		return Folded{Files: []File{{"a.json", "A"}}, Summary: "s", Report: []string{"### Check build", "", "a line"}}, nil
	}}}, r.deliver, func(string) {})
	if err != nil || !strings.Contains(r.calls[0].Body, "### Check build\n\na line") {
		t.Fatalf("%v %q", err, r.calls[0].Body)
	}
}
