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
// tagged with every one of Tags, the session's transcript ("" for none),
// and whether the license turned forced skill loading off.
type RunScope struct {
	Tags              []string
	Transcript        string
	SkipForcedLoading bool
}

// Checks runs the declared packs' checks.
type Checks interface {
	// Start begins building the checks binary and returns at once.
	Start(repo string) error
	// Run runs the checks scope selects, waiting up to wait for the
	// binary.
	Run(repo, event string, scope RunScope, wait time.Duration) CheckResult
}

// LicenseStatus is the session's license as one hook applies it.
type LicenseStatus struct {
	// Line is what SessionStart's context says of the license.
	Line string
	// Notice is a sentence to pass on to Claude once; empty when it was
	// already passed on or there is nothing to say.
	Notice string
	// WorkChecks is the gate's work-checks row.
	WorkChecks bool
	// ForcedLoading is the gate's forced-skill-loading row.
	ForcedLoading bool
	// State names the state for the stderr line (pending, ok, degraded).
	State string
	// Crumbs are license breadcrumbs this hook observed.
	Crumbs []string
}

// License reads the session's license state; it never waits on a network.
type License interface {
	// SessionStart applies a usable key or starts the background request.
	SessionStart(repo, sessionID string) LicenseStatus
	// Hook is every later hook's read of the same state.
	Hook(repo, sessionID string) LicenseStatus
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

// MissingImport is SessionStart's line for a member whose CLAUDE.md does
// not import the rules index, which leaves the session with no pack rules.
const MissingImport = `[cn] rules not loaded: CLAUDE.md does not import .claudinite/flat/claudinite-rules.GENERATED.md; add the line "@.claudinite/flat/claudinite-rules.GENERATED.md"`

// Handler answers hook events. A nil Checks runs no coded checks; a nil
// Guards judges no call; a nil License gates nothing; a nil Index writes
// no rules index.
type Handler struct {
	Checks  Checks
	Guards  Guards
	License License
	Index   RulesIndex
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
	}
	return h.perCall(event, raw, readErr, stdout, stderr, start)
}

// sessionKey is the state file's name for a session id; a hook input that
// carries none shares one file.
func sessionKey(id string) string {
	if id == "" {
		return "unknown"
	}
	return id
}

func (h Handler) sessionStart(repo, sessionID string, outcome breadcrumb.Outcome, stdout io.Writer, start time.Time) error {
	ctx := assemble(repo, h.engine())
	var b strings.Builder
	b.WriteString(HelloRule())
	b.WriteString("\n")
	for _, l := range ctx.notes {
		b.WriteString(l + "\n")
	}
	if h.Checks != nil {
		if err := h.Checks.Start(repo); err != nil {
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
	if h.License != nil {
		st := h.License.SessionStart(repo, sessionKey(sessionID))
		for _, c := range st.Crumbs {
			b.WriteString(c + "\n")
		}
		if st.Line != "" {
			b.WriteString(st.Line + "\n")
		}
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
	work := true
	scope := RunScope{Tags: []string{"work"}, Transcript: in.TranscriptPath}
	if h.License != nil {
		st := h.License.Hook(repo, sessionKey(in.SessionID))
		for _, c := range st.Crumbs {
			fmt.Fprintln(stderr, c)
		}
		if st.Notice != "" {
			fmt.Fprintln(stderr, st.Notice)
		}
		if !st.WorkChecks {
			work = false
			fmt.Fprintf(stderr, "[cn] license: work checks off (%s)\n", st.State)
		} else if !st.ForcedLoading {
			scope.SkipForcedLoading = true
			fmt.Fprintf(stderr, "[cn] license: forced skill loading off (%s)\n", st.State)
		}
	}
	if h.Checks != nil && work {
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
