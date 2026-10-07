package hooks

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
)

var crumb = regexp.MustCompile(`^\[cn\] hooks ([a-z-]+) (ok|error|crash|timeout|block|advise|nudge|deadline) [0-9]+ms$`)

type sessionStartOut struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func call(t *testing.T, event, stdin string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := Run(event, strings.NewReader(stdin), &out, &errb, time.Now())
	return out.String(), errb.String(), err
}

func sessionStart(t *testing.T, stdin string) (string, []string) {
	t.Helper()
	out, _, err := call(t, "session-start", stdin)
	if err != nil {
		t.Fatalf("session-start returned %v", err)
	}
	var got sessionStartOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if got.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Fatalf("hookEventName %q", got.HookSpecificOutput.HookEventName)
	}
	ctx := got.HookSpecificOutput.AdditionalContext
	lines := strings.Split(strings.TrimRight(ctx, "\n"), "\n")
	return ctx, lines
}

func TestSessionStartEmitsHelloRuleAndBreadcrumb(t *testing.T) {
	ctx, lines := sessionStart(t, `{"session_id":"abc","cwd":"/repo","hook_event_name":"SessionStart","source":"startup"}`)
	if !strings.Contains(ctx, "# Claudinite engine 0.0.0\n") {
		t.Errorf("no heading with the version: %q", ctx)
	}
	if !strings.Contains(ctx, "- Hello from cn: this rule proves the pinned engine loaded.") {
		t.Errorf("no hello bullet: %q", ctx)
	}
	last := lines[len(lines)-1]
	m := crumb.FindStringSubmatch(last)
	if m == nil || m[1] != "session-start" || m[2] != "ok" {
		t.Errorf("last line %q is not an ok session-start breadcrumb", last)
	}
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "[cn] ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d breadcrumb lines, want 1", n)
	}
}

func TestSessionStartSurvivesBadStdin(t *testing.T) {
	for _, stdin := range []string{"", "not json", `{"hook_event_name":"Stop"}`, `[1,2]`} {
		ctx, lines := sessionStart(t, stdin)
		if !strings.Contains(ctx, "Hello from cn") {
			t.Errorf("stdin %q: no hello rule", stdin)
		}
		m := crumb.FindStringSubmatch(lines[len(lines)-1])
		if m == nil || m[2] != "error" {
			t.Errorf("stdin %q: last line %q, want an error breadcrumb", stdin, lines[len(lines)-1])
		}
	}
}

func TestOtherEventsAnswerWithBreadcrumbOnly(t *testing.T) {
	for _, ev := range []string{"pre-tool-use", "post-tool-use", "user-prompt-submit", "stop", "session-end"} {
		out, errOut, err := call(t, ev, `{"hook_event_name":"x","tool_name":"Read"}`)
		if err != nil {
			t.Errorf("%s: %v", ev, err)
		}
		if strings.TrimSpace(out) != "{}" {
			t.Errorf("%s: stdout %q, want {}", ev, out)
		}
		lines := strings.Split(strings.TrimRight(errOut, "\n"), "\n")
		if len(lines) != 1 {
			t.Errorf("%s: stderr %q, want one line", ev, errOut)
			continue
		}
		if m := crumb.FindStringSubmatch(lines[0]); m == nil || m[1] != ev || m[2] != "ok" {
			t.Errorf("%s: stderr %q", ev, lines[0])
		}
	}
}

func TestUnknownEventIsUsage(t *testing.T) {
	out, _, err := call(t, "pre-compact-ish", "")
	if report.CodeOf(err) != report.Usage || err == nil {
		t.Fatalf("got %v", err)
	}
	if out != "" {
		t.Fatalf("stdout %q", out)
	}
}

// A session asks no license server and says nothing of a license.
func TestSessionStartSaysNothingOfALicense(t *testing.T) {
	ctx, _ := sessionStart(t, `{"session_id":"abc","cwd":"/repo","hook_event_name":"SessionStart","source":"startup"}`)
	if strings.Contains(strings.ToLower(ctx), "license") {
		t.Errorf("SessionStart mentions a license: %q", ctx)
	}
}
