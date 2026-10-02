package breadcrumb

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var shape = regexp.MustCompile(`^\[cn\] [a-z]+ [a-z-]+ (ok|error|crash|timeout|block|advise|nudge|deadline) [0-9]+ms$`)

func TestLineShape(t *testing.T) {
	cases := []struct {
		capability, event string
		outcome           Outcome
		d                 time.Duration
		want              string
	}{
		{"hooks", "session-start", OK, 12 * time.Millisecond, "[cn] hooks session-start ok 12ms"},
		{"hooks", "stop", Error, 1500 * time.Microsecond, "[cn] hooks stop error 1ms"},
		{"lifecycle", "selftest", Crash, 0, "[cn] lifecycle selftest crash 0ms"},
		{"hooks", "pre-tool-use", Timeout, 10 * time.Second, "[cn] hooks pre-tool-use timeout 10000ms"},
		{"hooks", "pre-tool-use", Block, 3 * time.Millisecond, "[cn] hooks pre-tool-use block 3ms"},
		{"hooks", "post-tool-use", Advise, 0, "[cn] hooks post-tool-use advise 0ms"},
		{"hooks", "user-prompt-submit", Nudge, 0, "[cn] hooks user-prompt-submit nudge 0ms"},
		{"hooks", "pre-tool-use", Deadline, 5 * time.Second, "[cn] hooks pre-tool-use deadline 5000ms"},
	}
	for _, c := range cases {
		got := Line(c.capability, c.event, c.outcome, c.d)
		if got != c.want {
			t.Errorf("Line = %q, want %q", got, c.want)
		}
		if !shape.MatchString(got) {
			t.Errorf("%q does not match the breadcrumb shape", got)
		}
	}
}

// Whatever a caller passes, the line never carries free text: a field that
// is not a plain token is replaced, so error text cannot leak into it.
func TestNoFreeTextEverAppears(t *testing.T) {
	hostile := []struct{ capability, event string }{
		{"hooks", "open /etc/passwd: permission denied"},
		{"Hooks", "stop"},
		{"hooks\n[cn] fake", "stop"},
		{"", ""},
		{"hooks", "session-start ok 1ms\nleak"},
	}
	for _, h := range hostile {
		got := Line(h.capability, h.event, Outcome("panic: boom"), -time.Second)
		if !shape.MatchString(got) {
			t.Errorf("Line(%q, %q) = %q escapes the shape", h.capability, h.event, got)
		}
		for _, leak := range []string{"passwd", "denied", "boom", "panic", "leak", "fake"} {
			if strings.Contains(got, leak) {
				t.Errorf("Line leaked %q: %q", leak, got)
			}
		}
	}
}
