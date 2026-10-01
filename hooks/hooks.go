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

// Checks runs the declared packs' coded checks.
type Checks interface {
	// Start begins building the checks binary and returns at once.
	Start(repo string) error
	// Run runs the checks tagged with every one of tags, waiting up to
	// wait for the binary.
	Run(repo, event string, tags []string, wait time.Duration) CheckResult
}

// Handler answers hook events. A nil Checks runs no coded checks.
type Handler struct {
	Checks Checks
	// ProjectDir overrides where the repo is found.
	ProjectDir string
	// Engine overrides this engine's version, for tests.
	Engine string
}

type hookInput struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"`
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
}

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
		return h.sessionStart(h.projectDir(in), outcome, stdout, start)
	case "stop":
		return h.stop(h.projectDir(in), in.StopHookActive, stdout, stderr, start)
	}
	fmt.Fprintln(stdout, "{}")
	fmt.Fprintln(stderr, breadcrumb.Line("hooks", event, breadcrumb.OK, time.Since(start)))
	return nil
}

func (h Handler) sessionStart(repo string, outcome breadcrumb.Outcome, stdout io.Writer, start time.Time) error {
	ctx := assemble(repo, h.engine())
	var b strings.Builder
	b.WriteString(ctx.rules)
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
func (h Handler) stop(repo string, active bool, stdout, stderr io.Writer, start time.Time) error {
	answer := "{}"
	if h.Checks != nil {
		res := h.Checks.Run(repo, "stop", []string{"work"}, StopWait)
		for _, e := range res.Errors {
			fmt.Fprintln(stderr, "[cn] check error: "+e)
		}
		if res.Err != nil {
			fmt.Fprintf(stderr, "[cn] checks did not run: %v\n", res.Err)
		}
		if findings.AnyBreak(res.Findings) && !active {
			var reason strings.Builder
			reason.WriteString("Claudinite checks found work to finish before stopping:\n")
			for _, f := range res.Findings {
				fmt.Fprintf(&reason, "- %s %s: %s\n", f.ID, f.Path, f.Sentence)
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
