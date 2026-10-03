package precondition

import (
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestLogPastRetentionOverEveryLogsShape(t *testing.T) {
	cases := []struct {
		name   string
		logs   *Logs
		holds  bool
		reason string
	}{
		{"no reading", nil, true, "the worker decides"},
		{"absent branch", &Logs{}, false, "no conversation-logs branch"},
		{"no logs", &Logs{Present: true}, false, "no log older than retention 10d"},
		{"within the default window", &Logs{Present: true, OldestLogAgeDays: f(9.5)}, false, "retention 10d"},
		{"past the default window", &Logs{Present: true, OldestLogAgeDays: f(10.25)}, true, "oldest log 10.3d old vs retention 10d"},
		{"exactly the window", &Logs{Present: true, OldestLogAgeDays: f(10)}, false, "nothing to prune"},
		{"past a declared window", &Logs{Present: true, RetentionDays: f(3), OldestLogAgeDays: f(4)}, true, "vs retention 3d"},
		{"retention off", &Logs{Present: true, RetentionDays: f(0), OldestLogAgeDays: f(400)}, false, "retention_days is 0 — capture-only"},
		{"negative retention", &Logs{Present: true, RetentionDays: f(-1), OldestLogAgeDays: f(400)}, false, "retention_days is -1"},
		{"unreadable retention", &Logs{Present: true, RetentionUnreadable: true, OldestLogAgeDays: f(400)}, false, "retention_days is unreadable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := logPastRetention(Signals{ConversationLogs: c.logs}, Opts{})
			if o.Holds != c.holds || !strings.Contains(o.Reason, c.reason) || o.Error != "" {
				t.Fatalf("%+v", o)
			}
		})
	}
	if !EngineJudged("log-past-retention") {
		t.Fatal("the runner would ask a preconditions.mjs for an engine term")
	}
}
