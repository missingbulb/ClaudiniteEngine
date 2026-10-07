package hooks

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/breadcrumb"
)

type fakeGrowth struct {
	calls   []string
	outcome breadcrumb.Outcome
	say     string
	panics  bool
	block   chan struct{}
}

func (f *fakeGrowth) Capture(repo, session, transcript string, issue int, out io.Writer) breadcrumb.Outcome {
	f.calls = append(f.calls, fmt.Sprintf("%s %s %d", session, transcript, issue))
	if f.block != nil {
		<-f.block
	}
	if f.panics {
		panic("boom")
	}
	fmt.Fprintln(out, f.say)
	return f.outcome
}

const endIn = `{"hook_event_name":"SessionEnd","session_id":"s1","transcript_path":"/t/s1.jsonl"}`

func endsClean(t *testing.T, out string) {
	t.Helper()
	if strings.TrimSpace(out) != "{}" {
		t.Fatalf("stdout %q, want {}", out)
	}
}

func TestSessionEndCapturesUnderTheSessionIssue(t *testing.T) {
	repo := member(t, []string{GrowthPack}, nil)
	g := &fakeGrowth{outcome: breadcrumb.OK, say: "captured 3 entries → x on conversation-logs"}
	t.Setenv("CLAUDINITE_SESSION_ISSUE", "772")
	out, errOut := hook(t, Handler{ProjectDir: repo, Growth: g}, "session-end", endIn)
	endsClean(t, out)
	if len(g.calls) != 1 || g.calls[0] != "s1 /t/s1.jsonl 772" {
		t.Fatalf("calls %v", g.calls)
	}
	if !strings.Contains(errOut, g.say) || !strings.Contains(errOut, "[cn] growth capture ok ") {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestAFailingCaptureStillEndsTheSession(t *testing.T) {
	repo := member(t, []string{GrowthPack}, nil)
	for _, g := range []*fakeGrowth{{outcome: breadcrumb.Error, say: "could not push"}, {panics: true}} {
		out, errOut := hook(t, Handler{ProjectDir: repo, Growth: g}, "session-end", endIn)
		endsClean(t, out)
		if !strings.Contains(errOut, "[cn] growth capture error ") || !strings.Contains(errOut, "[cn] hooks session-end ok ") {
			t.Fatalf("stderr %q", errOut)
		}
	}
}

func TestACaptureOverItsBoundIsLeftBehind(t *testing.T) {
	repo := member(t, []string{GrowthPack}, nil)
	was := CaptureBound
	CaptureBound = 20 * time.Millisecond
	defer func() { CaptureBound = was }()
	g := &fakeGrowth{block: make(chan struct{})}
	defer close(g.block)
	out, errOut := hook(t, Handler{ProjectDir: repo, Growth: g}, "session-end", endIn)
	endsClean(t, out)
	if !strings.Contains(errOut, "did not finish within") || !strings.Contains(errOut, "[cn] growth capture timeout ") {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestWithoutTheGrowthPackNothingIsCaptured(t *testing.T) {
	for _, repo := range []string{member(t, []string{"acme-pack"}, nil), member(t, nil, nil), t.TempDir()} {
		g := &fakeGrowth{}
		out, errOut := hook(t, Handler{ProjectDir: repo, Growth: g}, "session-end", endIn)
		endsClean(t, out)
		if len(g.calls) != 0 || strings.Contains(errOut, "growth") {
			t.Fatalf("calls %v stderr %q", g.calls, errOut)
		}
	}
}

func TestSessionIssueReadsOnlyANonNegativeInteger(t *testing.T) {
	for raw, want := range map[string]int{"772": 772, " 5 ": 5, "": 0, "not-a-number": 0, "-3": 0, "1.5": 0} {
		if got := SessionIssue(raw); got != want {
			t.Errorf("SessionIssue(%q) = %d, want %d", raw, got, want)
		}
	}
}
