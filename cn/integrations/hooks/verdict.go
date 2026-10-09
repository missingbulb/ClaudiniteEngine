package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
)

// HookDeadline bounds a per-call hook's judging: past it the call goes
// through.
const HookDeadline = 5 * time.Second

// GuardBuildWait bounds a per-call hook's wait, before its judging, for a
// checks build another process holds; one this hook runs itself takes as
// long as it takes.
const GuardBuildWait = 45 * time.Second

// deadlineEnv overrides HookDeadline in milliseconds, for tests.
const deadlineEnv = "CLAUDINITE_HOOK_DEADLINE_MS"

func hookDeadline() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv(deadlineEnv)); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return HookDeadline
}

// Call is one per-call hook's subject: the tool call about to run or just
// run, or the owner's prompt, with the session's transcript.
type Call struct {
	// Event is cn's event name.
	Event string
	Tool  string
	// Input and Response are the payload's tool_input and tool_response
	// as sent, nil when absent.
	Input, Response json.RawMessage
	Prompt          string
	Session         *transcript.Session
}

// GuardResult is what the guards say of a call: block lines, advisory
// context lines, and stderr notes (a guard that could not decide).
type GuardResult struct {
	Blocks, Advice, Notes []string
}

// Guards judges a call through the declared action checks, the built-in
// guards and the packs' coded judges, never past deadline.
type Guards interface {
	// Prepare builds the coded judges' binary unless it is built, and
	// returns the build's breadcrumbs.
	Prepare(repo, event string) ([]string, error)
	Judge(repo string, call Call, deadline time.Time) GuardResult
}

// verdict is one per-call hook's answer.
type verdict struct {
	block   []string
	context []string
	notes   []string
	outcome breadcrumb.Outcome
}

type callInput struct {
	SessionID      string          `json:"session_id"`
	Cwd            string          `json:"cwd"`
	TranscriptPath string          `json:"transcript_path"`
	ToolName       *string         `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	Prompt         any             `json:"prompt"`
}

// perCall answers pre-tool-use, post-tool-use and user-prompt-submit. A
// block on PreToolUse goes to stderr and exits 2; a block
// elsewhere cannot block and is passed on as context; context goes out as
// additionalContext; a hook that cannot decide, or runs past its
// deadline, answers {} and lets the call through. Every path ends with
// the breadcrumb.
func (h Handler) perCall(event string, raw []byte, readErr error, stdout, stderr io.Writer, start time.Time) error {
	name := contextEvents[event]
	var in callInput
	parseErr := json.Unmarshal(raw, &in)
	repo := h.projectDir(hookInput{Cwd: in.Cwd})
	fail := func(outcome breadcrumb.Outcome, notes ...string) error {
		for _, n := range notes {
			fmt.Fprintln(stderr, n)
		}
		fmt.Fprintln(stdout, "{}")
		fmt.Fprintln(stderr, breadcrumb.Line("hooks", event, outcome, time.Since(start)))
		return nil
	}
	if readErr != nil || parseErr != nil {
		return fail(breadcrumb.Error, "[cn] hooks: the payload is not JSON")
	}
	if event != "user-prompt-submit" && in.ToolName == nil {
		return fail(breadcrumb.Error, "[cn] hooks: the payload names no tool_name")
	}
	call := Call{Event: event, Input: present(in.ToolInput), Response: present(in.ToolResponse), Session: transcript.NewSession(in.TranscriptPath)}
	if in.ToolName != nil {
		call.Tool = *in.ToolName
	}
	if p, ok := in.Prompt.(string); ok {
		call.Prompt = p
	}
	var built []string
	if h.Guards != nil {
		lines, err := h.Guards.Prepare(repo, event)
		built = lines
		if err != nil {
			built = append(built, "[cn] hooks: the checks binary is not ready: "+err.Error())
		}
	}
	limit := hookDeadline()
	deadline := time.Now().Add(limit)
	done := make(chan verdict, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- verdict{outcome: breadcrumb.Error, notes: []string{fmt.Sprintf("[cn] hooks: the %s judge failed: %v", event, r)}}
			}
		}()
		done <- h.judge(repo, call, deadline)
	}()
	var v verdict
	select {
	case v = <-done:
	case <-time.After(time.Until(deadline)):
		return fail(breadcrumb.Deadline, append(built, fmt.Sprintf("[cn] hooks: no verdict within %v; the call goes through", limit))...)
	}
	v.notes = append(built, v.notes...)
	if v.outcome == breadcrumb.Error && len(v.block) == 0 && len(v.context) == 0 {
		return fail(breadcrumb.Error, v.notes...)
	}
	if len(v.block) > 0 && event == "pre-tool-use" {
		fmt.Fprintln(stderr, strings.Join(v.block, "\n"))
		for _, n := range v.notes {
			fmt.Fprintln(stderr, n)
		}
		fmt.Fprintln(stderr, breadcrumb.Line("hooks", event, breadcrumb.Block, time.Since(start)))
		return report.New(report.Block, "the call is blocked")
	}
	if len(v.block) > 0 {
		v.context = append(append([]string{}, v.block...), v.context...)
		v.notes = append(v.notes, fmt.Sprintf("[cn] hooks: a block on %s cannot block; it is passed on as context", event))
		v.outcome = breadcrumb.Error
	}
	answer := "{}"
	if len(v.context) > 0 {
		var out sessionStartOutput
		out.HookSpecificOutput.HookEventName = name
		out.HookSpecificOutput.AdditionalContext = strings.Join(v.context, "\n")
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(out)
		answer = strings.TrimRight(b.String(), "\n")
	}
	for _, n := range v.notes {
		fmt.Fprintln(stderr, n)
	}
	fmt.Fprintln(stdout, answer)
	fmt.Fprintln(stderr, breadcrumb.Line("hooks", event, v.outcome, time.Since(start)))
	return nil
}

// present is a raw JSON field, nil when absent.
func present(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// judge is the event's verdict: forced-loading holds and nudges, then the
// guards.
func (h Handler) judge(repo string, call Call, deadline time.Time) verdict {
	v := verdict{outcome: breadcrumb.OK}
	triggers := derive(repo, h.engine())
	switch call.Event {
	case "pre-tool-use":
		if hold := triggers.hold(repo, call); hold != "" {
			v.block, v.outcome = []string{hold}, breadcrumb.Block
			return v
		}
	case "user-prompt-submit":
		if n := triggers.promptNudges(repo, call); len(n) > 0 {
			v.context, v.outcome = n, breadcrumb.Nudge
		}
	case "post-tool-use":
		if n := triggers.resultNudges(repo, call); len(n) > 0 {
			v.context, v.outcome = n, breadcrumb.Nudge
		}
	}
	if h.Guards == nil {
		return v
	}
	g := h.Guards.Judge(repo, call, deadline)
	v.notes = append(v.notes, g.Notes...)
	if len(g.Blocks) > 0 {
		v.block, v.outcome = g.Blocks, breadcrumb.Block
	}
	if len(g.Advice) > 0 {
		v.context = append(v.context, g.Advice...)
		if v.outcome == breadcrumb.OK {
			v.outcome = breadcrumb.Advise
		}
	}
	return v
}
