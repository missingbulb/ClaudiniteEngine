package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Events are the hook events cn answers, as cn hook <event> spells them.
var Events = []string{"session-start", "pre-tool-use", "post-tool-use", "user-prompt-submit", "stop", "session-end"}

// maxStdin bounds what a hook reads from Claude Code.
const maxStdin = 16 << 20

// StopWait is how long Stop waits for the checks binary to be ready.
const StopWait = 30 * time.Second

// HelloRule is the rule content this engine version carries itself.
func HelloRule() string {
	return fmt.Sprintf("# Claudinite engine %s\n\n- Hello from cn: this rule proves the pinned engine loaded.\n", version.Version())
}

// CheckResult is one run of the packs' coded checks. Crumb is the checks
// breadcrumb line; Err is set when the run did not happen.
type CheckResult struct {
	Findings []findings.Finding
	Errors   []string
	Err      error
	Crumb    string
}

// RunScope is which checks one run takes and what they read: the checks
// tagged with every one of Tags, and the session's transcript ("" for
// none).
type RunScope struct {
	Tags       []string
	Transcript string
	// Session is the Claude Code session id, "" for none.
	Session string
}

// Checks runs the declared packs' checks.
type Checks interface {
	// Start begins building the checks binary for the session and returns
	// at once, with the build's breadcrumb ("" for nothing to build).
	Start(repo, session string) (string, error)
	// Run runs the checks scope selects, waiting up to wait for the
	// binary.
	Run(repo, event string, scope RunScope, wait time.Duration) CheckResult
}

// RulesIndex writes the generated index the member's CLAUDE.md imports, so
// a session on a hand-edited declaration gets its rules on the next start.
type RulesIndex interface {
	Write(repo, engine string) (bool, error)
	// HasImport reports whether the member's CLAUDE.md imports the index.
	HasImport(repo string) bool
	// HasRules reports whether the index imports any prose.
	HasRules(repo, engine string) bool
}

// UserPack is SessionStart's first step: where the user-pack pack is
// declared, the person's own pack copied into the session's temp pack
// root before the pack set is read. It returns its one line, "" where the
// pack is not declared; it never fails the hook.
type UserPack interface {
	Prepare(repo string) string
}

// MissingImport is SessionStart's line for a member whose CLAUDE.md does
// not import the rules index, which leaves the session with no pack rules.
const MissingImport = `[cn] rules not loaded: CLAUDE.md does not import .claudinite/cache/claudinite-rules.GENERATED.md; add the line "@.claudinite/cache/claudinite-rules.GENERATED.md"`

// Handler answers hook events. A nil Checks runs no coded checks; a nil
// Guards judges no call; a nil Index writes no rules index; a nil Growth captures nothing; a nil UserPack copies
// nothing.
type Handler struct {
	Checks Checks
	Guards Guards
	Index  RulesIndex
	Growth Growth
	// UserPack copies the person's pack in at SessionStart; nil copies
	// nothing.
	UserPack UserPack
	// ProjectDir overrides where the repo is found.
	ProjectDir string
	// Engine overrides this engine's version, for tests.
	Engine string
}

type hookInput struct {
	SessionID      string `json:"session_id"`
	Source         string `json:"source"`
	HookEventName  string `json:"hook_event_name"`
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
	TranscriptPath string `json:"transcript_path"`
}

// contextEvents are the events whose answer may carry additionalContext,
// by cn's event name and Claude Code's.
var contextEvents = map[string]string{"pre-tool-use": "PreToolUse", "post-tool-use": "PostToolUse", "user-prompt-submit": "UserPromptSubmit"}

type sessionStartOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// Run answers one hook event with no coded checks.
func Run(event string, stdin io.Reader, stdout, stderr io.Writer, start time.Time) error {
	return Handler{}.Run(event, stdin, stdout, stderr, start)
}

func (h Handler) projectDir(in hookInput) string {
	switch {
	case h.ProjectDir != "":
		return h.ProjectDir
	case os.Getenv("CLAUDE_PROJECT_DIR") != "":
		return os.Getenv("CLAUDE_PROJECT_DIR")
	case in.Cwd != "":
		return in.Cwd
	}
	wd, _ := os.Getwd()
	return wd
}

func (h Handler) engine() string {
	if h.Engine != "" {
		return h.Engine
	}
	return version.Version()
}

// Run answers one hook event. Only an unknown event is an error (usage);
// everything else answers and records its outcome in the breadcrumb.
func (h Handler) Run(event string, stdin io.Reader, stdout, stderr io.Writer, start time.Time) error {
	known := false
	for _, e := range Events {
		known = known || e == event
	}
	if !known {
		return report.New(report.Usage, fmt.Sprintf("unknown hook event %q", event))
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdin, maxStdin))
	var in hookInput
	parseErr := json.Unmarshal(raw, &in)
	switch event {
	case "session-start":
		outcome := breadcrumb.OK
		if readErr != nil || parseErr != nil || in.HookEventName != "SessionStart" {
			outcome = breadcrumb.Error
		}
		return h.sessionStart(h.projectDir(in), in.SessionID, outcome, stdout, start)
	case "stop":
		return h.stop(h.projectDir(in), in, stdout, stderr, start)
	case "session-end":
		return h.sessionEnd(in, parseErr == nil && readErr == nil, stdout, stderr, start)
	}
	return h.perCall(event, raw, readErr, stdout, stderr, start)
}

func (h Handler) sessionStart(repo, session string, outcome breadcrumb.Outcome, stdout io.Writer, start time.Time) error {
	personal := ""
	if h.UserPack != nil {
		personal = h.UserPack.Prepare(repo)
	}
	ctx := assemble(repo, h.engine())
	var b strings.Builder
	b.WriteString(HelloRule())
	b.WriteString("\n")
	if personal != "" {
		b.WriteString(personal + "\n")
	}
	for _, l := range ctx.notes {
		b.WriteString(l + "\n")
	}
	if h.Checks != nil {
		line, err := h.Checks.Start(repo, session)
		if line != "" {
			b.WriteString(line + "\n")
		}
		if err != nil {
			fmt.Fprintf(&b, "[cn] checks build did not start: %v\n", err)
		}
	}
	if h.Index != nil && ctx.selfCheck != "" {
		if _, err := h.Index.Write(repo, h.engine()); err != nil {
			fmt.Fprintf(&b, "[cn] rules index not written: %v\n", err)
		}
		if !h.Index.HasImport(repo) && h.Index.HasRules(repo, h.engine()) {
			b.WriteString(MissingImport + "\n")
		}
	}
	if ctx.selfCheck != "" {
		b.WriteString(ctx.selfCheck + "\n")
	}
	b.WriteString(breadcrumb.Line("hooks", "session-start", outcome, time.Since(start)) + "\n")
	var out sessionStartOutput
	out.HookSpecificOutput.HookEventName = "SessionStart"
	out.HookSpecificOutput.AdditionalContext = b.String()
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

// stop runs the work checks. A blocking finding answers Claude Code's
// block form, unless Claude Code is already continuing because of a stop
// hook (stop_hook_active), when the findings go to stderr instead so a
// finding the session cannot clear never loops.
func (h Handler) stop(repo string, in hookInput, stdout, stderr io.Writer, start time.Time) error {
	answer := "{}"
	scope := RunScope{Tags: []string{"work"}, Transcript: in.TranscriptPath, Session: in.SessionID}
	if h.Checks != nil {
		res := h.Checks.Run(repo, "stop", scope, StopWait)
		for _, e := range res.Errors {
			fmt.Fprintln(stderr, "[cn] check error: "+e)
		}
		if res.Err != nil {
			fmt.Fprintf(stderr, "[cn] checks did not run: %v\n", res.Err)
		}
		if findings.AnyBreak(res.Findings) && !in.StopHookActive {
			var reason strings.Builder
			reason.WriteString("Claudinite checks found work to finish before stopping:\n")
			for _, f := range res.Findings {
				fmt.Fprintf(&reason, "- %s %s: %s\n", f.Name(), f.Location(), f.Sentence)
				if f.Fix != "" {
					fmt.Fprintf(&reason, "  fix: %s\n", f.Fix)
				}
			}
			out, _ := json.Marshal(struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason"`
			}{"block", strings.TrimRight(reason.String(), "\n")})
			answer = string(out)
		} else {
			findings.Print(stderr, res.Findings)
		}
		if res.Crumb != "" {
			fmt.Fprintln(stderr, res.Crumb)
		}
	}
	fmt.Fprintln(stdout, answer)
	fmt.Fprintln(stderr, breadcrumb.Line("hooks", "stop", breadcrumb.OK, time.Since(start)))
	return nil
}
