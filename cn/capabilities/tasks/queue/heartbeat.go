package queue

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
)

// The holder's sign of life. A work step may run for hours, and while it
// does the item goes dark; so the executor comments on its own item at a
// fixed interval, and the reclaim measures silence from that, never from
// the issue's updated_at, which any comment moves, a loser's struck claim
// included.

// HeartbeatEvery is comfortably inside the executing leash, so a live
// holder is never reclaimed on one missed beat.
const HeartbeatEvery = workitem.Heartbeat

// AgentBeatEvery is the ceiling an agent session is asked to beat under.
const AgentBeatEvery = 45 * time.Minute

// HeartbeatComment is the executor's beat.
func HeartbeatComment(executor, at string, minutes int) string {
	return fmt.Sprintf("%s\nStill working: executor `%s`, %d minute(s) in, at %s.", workitem.HeartbeatMarker, executor, minutes, at)
}

// AgentBeatComment is an agent session's beat; it carries the same marker,
// so the liveness read counts it without knowing which phase wrote it.
func AgentBeatComment(session, at, note string) string {
	s := ""
	if session != "" {
		s = ": " + session
	}
	return fmt.Sprintf("%s\nStill working%s — %s, at %s.", workitem.HeartbeatMarker, s, note, at)
}

func instant(s string) (time.Time, bool) {
	t, ok := calendar.ParseInstant(s)
	return t, ok && t.UnixMilli() > 0
}

// LastLivenessAt is when the holder last said anything: the newest live
// claim or heartbeat, a struck one excluded; zero when there is none.
func LastLivenessAt(comments []world.Comment) time.Time {
	var newest time.Time
	for _, c := range comments {
		if strings.Contains(c.Body, workitem.EpisodeMarker) {
			continue
		}
		if !strings.Contains(c.Body, workitem.ClaimMarker) && !strings.Contains(c.Body, workitem.HeartbeatMarker) {
			continue
		}
		if t, ok := instant(c.CreatedAt); ok && t.After(newest) {
			newest = t
		}
	}
	return newest
}

var beatNoteRE = regexp.MustCompile(` — (.*), at [^,]*\.$`)

func beatNote(body string) string {
	line := ""
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "Still working") {
			line = l
			break
		}
	}
	if m := beatNoteRE.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(line)
}

// LastProgressAt is when the work last moved, as distinct from when the
// holder last spoke: the oldest beat of the trailing run that all carry the
// newest beat's note. Zero when the item has no beats, the caller's signal
// to judge it the old way rather than call it dead.
func LastProgressAt(comments []world.Comment) time.Time {
	type beat struct {
		at   time.Time
		note string
	}
	var beats []beat
	for _, c := range comments {
		if !strings.Contains(c.Body, workitem.HeartbeatMarker) || strings.Contains(c.Body, workitem.EpisodeMarker) {
			continue
		}
		if t, ok := instant(c.CreatedAt); ok {
			beats = append(beats, beat{t, beatNote(c.Body)})
		}
	}
	if len(beats) == 0 {
		return time.Time{}
	}
	sort.SliceStable(beats, func(a, b int) bool { return beats[a].at.Before(beats[b].at) })
	newest := beats[len(beats)-1].note
	i := len(beats) - 1
	for i > 0 && beats[i-1].note == newest {
		i--
	}
	return beats[i].at
}

// WithProgress appends one line to the body's Progress section: the body
// is the only surface a session can grow in place.
func WithProgress(body, line string) string {
	return workitem.WithSection(body, workitem.ProgressHeading, append(workitem.ProgressLines(body), line))
}

// Ticker is the timer the beat hangs on, a seam so a harness running a
// work step in virtual minutes still sees its beats.
type Ticker interface {
	Every(d time.Duration, fn func()) (stop func())
}

// RealTicker fires on the wall clock.
type RealTicker struct{}

// Every runs fn every d until stop is called.
func (RealTicker) Every(d time.Duration, fn func()) func() {
	t := time.NewTicker(d)
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		for {
			select {
			case <-t.C:
				fn()
			case <-done:
				return
			}
		}
	}()
	return func() { t.Stop(); close(done); <-exited }
}

// WithHeartbeat runs work while beating every interval. A beat that fails
// never sinks the work, but is logged: a run whose beat failed is one the
// leash may reclaim underneath it.
func WithHeartbeat(work func() error, beat func(minutes int) error, every time.Duration, clock world.Clock, ticker Ticker, log func(string)) error {
	if beat == nil || every <= 0 {
		return work()
	}
	start := clock.Now()
	var mu sync.Mutex
	beats := 0
	minutes := func() int { return int(clock.Now().Sub(start).Round(time.Minute) / time.Minute) }
	stop := ticker.Every(every, func() {
		mu.Lock()
		defer mu.Unlock()
		if err := beat(minutes()); err != nil {
			log(fmt.Sprintf("! heartbeat %d did not post (%v) — the leash may reclaim this item mid-work", beats+1, err))
			return
		}
		beats++
	})
	err := work()
	stop()
	mu.Lock()
	defer mu.Unlock()
	if beats > 0 {
		log(fmt.Sprintf("- work step ran %d minute(s) and beat %d time(s)", minutes(), beats))
	}
	return err
}
