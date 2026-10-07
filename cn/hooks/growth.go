// The session-end capture. With stdin that does not parse, the hook has
// no session id or transcript path and falls through to discovery, which
// captures the newest transcript anywhere under ~/.claude/projects, as
// Node did (the newest-anywhere findtranscript fixture). A manual run on
// a developer machine can therefore push an unrelated project's
// transcript.

package hooks

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// Growth captures a session onto the conversation-logs branch.
type Growth interface {
	// Capture runs one capture filed under issue (0 for none), saying
	// what it did on out, and returns its breadcrumb outcome. An empty
	// session or transcript is left to discovery.
	Capture(repo, session, transcript string, issue int, out io.Writer) breadcrumb.Outcome
}

// GrowthPack is the pack whose declaration turns the capture on.
const GrowthPack = "claudinite-growth"

// CaptureBound is how long the session-end capture may run before the
// session ends without it.
var CaptureBound = 120 * time.Second

var sessionIssue = regexp.MustCompile(`^\d+$`)

// SessionIssue is the issue CLAUDINITE_SESSION_ISSUE names, 0 where it
// names none: a hook firing never knows what its session was for, so
// only an explicit invocation sets it.
func SessionIssue(raw string) int {
	raw = strings.TrimSpace(raw)
	if !sessionIssue.MatchString(raw) {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

// growthDeclared reports whether the repo's declaration names the growth
// pack.
func growthDeclared(repo string) bool {
	path, f, err := settings.Find(repo)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return false
	}
	for _, e := range p.Entries {
		if !e.Local && e.ID == GrowthPack {
			return true
		}
	}
	return false
}

// sessionEnd is the capture event: where the growth pack is declared it
// captures the session under CLAUDINITE_SESSION_ISSUE or issue 0. A
// session must never fail to end because of it, so every failure is one
// stderr line and the answer is always {} and exit 0.
func (h Handler) sessionEnd(in hookInput, parsed bool, stdout, stderr io.Writer, start time.Time) error {
	defer func() {
		fmt.Fprintln(stdout, "{}")
		fmt.Fprintln(stderr, breadcrumb.Line("hooks", "session-end", breadcrumb.OK, time.Since(start)))
	}()
	if !parsed {
		in = hookInput{}
	}
	repo := h.projectDir(in)
	if h.Growth == nil || !growthDeclared(repo) {
		return nil
	}
	began := time.Now()
	issue := SessionIssue(os.Getenv("CLAUDINITE_SESSION_ISSUE"))
	done := make(chan breadcrumb.Outcome, 1)
	var said strings.Builder
	out := &lockedWriter{w: &said}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(out, "[cn] growth: the capture failed: %v\n", r)
				done <- breadcrumb.Error
			}
		}()
		done <- h.Growth.Capture(repo, in.SessionID, in.TranscriptPath, issue, out)
	}()
	var outcome breadcrumb.Outcome
	select {
	case outcome = <-done:
	case <-time.After(CaptureBound):
		outcome = breadcrumb.Timeout
		fmt.Fprintf(out, "[cn] growth: the capture did not finish within %v; the session ends without it\n", CaptureBound)
	}
	fmt.Fprint(stderr, out.String())
	fmt.Fprintln(stderr, breadcrumb.Line("growth", "capture", outcome, time.Since(began)))
	return nil
}

// lockedWriter collects a capture's sentences, which a capture past its
// bound may still be writing when the hook reads them.
type lockedWriter struct {
	mu sync.Mutex
	w  *strings.Builder
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}
