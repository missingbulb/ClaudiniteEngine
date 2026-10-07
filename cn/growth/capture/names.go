package capture

import (
	"regexp"
	"strconv"
	"time"
)

// DefaultBranch is the orphan branch captures land on.
const DefaultBranch = "conversation-logs"

// Key is what a capture is filed under: the pull request a merge landed,
// or an issue (0 for none) for a capture no merge produced. Exactly one
// is set.
type Key struct {
	PR, Issue *int
}

// LogFilename is a capture's file name, <stamp>--pr-<n>--<session>.jsonl
// or <stamp>--issue-<n>--<session>.jsonl: a minute stamp first, so a bare
// listing sorts and ages by name, then the key, then the session id the
// delta looks up by.
func LogFilename(now time.Time, k Key, session string) string {
	stamp := now.UTC().Format("2006-01-02T1504Z")
	key := ""
	if k.PR != nil && *k.PR > 0 {
		key = "pr-" + strconv.Itoa(*k.PR)
	} else {
		issue := 0
		if k.Issue != nil {
			issue = *k.Issue
		}
		key = "issue-" + strconv.Itoa(issue)
	}
	return stamp + "--" + key + "--" + session + ".jsonl"
}

var logName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})T(\d{2})(\d{2})Z(?:-\d+)?--(pr|issue)-(\d+)--(.+)\.jsonl$`)

// Parsed is a capture's file name read back. The key it was not filed
// under is nil.
type Parsed struct {
	CapturedAt string `json:"capturedAt"`
	Issue      *int   `json:"issue"`
	PR         *int   `json:"pr"`
	SessionID  string `json:"sessionId"`
}

// ParseLogFilename reads a capture's file name; false for any other file.
func ParseLogFilename(name string) (Parsed, bool) {
	m := logName.FindStringSubmatch(name)
	if m == nil {
		return Parsed{}, false
	}
	n, err := strconv.Atoi(m[5])
	if err != nil {
		return Parsed{}, false
	}
	p := Parsed{CapturedAt: m[1] + "T" + m[2] + ":" + m[3] + ":00Z", SessionID: m[6]}
	if m[4] == "issue" {
		p.Issue = &n
	} else {
		p.PR = &n
	}
	return p, true
}
