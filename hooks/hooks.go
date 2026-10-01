package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Events are the hook events cn answers, as cn hook <event> spells them.
var Events = []string{"session-start", "pre-tool-use", "post-tool-use", "user-prompt-submit", "stop", "session-end"}

// maxStdin bounds what a hook reads from Claude Code.
const maxStdin = 16 << 20

// HelloRule is the only rule content this engine version carries.
func HelloRule() string {
	return fmt.Sprintf("# Claudinite engine %s\n\n- Hello from cn: this rule proves the pinned engine loaded.\n", version.Version())
}

type sessionStartInput struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
}

// Run answers one hook event. Only an unknown event is an error (usage);
// everything else answers and records its outcome in the breadcrumb.
func Run(event string, stdin io.Reader, stdout, stderr io.Writer, start time.Time) error {
	known := false
	for _, e := range Events {
		known = known || e == event
	}
	if !known {
		return report.New(report.Usage, fmt.Sprintf("unknown hook event %q", event))
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdin, maxStdin))
	if event != "session-start" {
		fmt.Fprintln(stdout, "{}")
		fmt.Fprintln(stderr, breadcrumb.Line("hooks", event, breadcrumb.OK, time.Since(start)))
		return nil
	}

	outcome := breadcrumb.OK
	var in sessionStartInput
	if readErr != nil || json.Unmarshal(raw, &in) != nil || in.HookEventName != "SessionStart" {
		outcome = breadcrumb.Error
	}
	var out struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	out.HookSpecificOutput.HookEventName = "SessionStart"
	out.HookSpecificOutput.AdditionalContext = HelloRule() + "\n" +
		breadcrumb.Line("hooks", event, outcome, time.Since(start)) + "\n"
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
