package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks"
	checkscmd "github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/command"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/rules/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/capture"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/hooks"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/userpack"
)

// A hook composes every segment at one Claude Code event: the checks and
// guards, the rules index, the growth capture and the user's own pack. The
// adapters handing each segment's service to the hooks live here.

// runHook is a variable so a test can make a hook panic.
var runHook = hooks.Handler{Checks: hookChecks{}, Guards: hookGuards{}, Index: hookIndex{}, Growth: hookGrowth{}, UserPack: hookUserPack{}}.Run

// hookChecks gives the hooks the checks service.
type hookChecks struct{}

func (hookChecks) Start(repo, session string) (string, error) {
	svc := checkscmd.Service()
	svc.Session = session
	return svc.Start(repo)
}

func (hookChecks) Run(repo, event string, scope hooks.RunScope, wait time.Duration) hooks.CheckResult {
	var notes bytes.Buffer
	sel := declared.Selection{Tags: scope.Tags, Session: transcript.NewSession(scope.Transcript)}
	svc := checkscmd.Service()
	svc.Session = scope.Session
	svc.Timing = func(line string) { notes.WriteString(line + "\n") }
	o := svc.RunAll(repo, event, sel, wait, false, &notes)
	crumb := strings.TrimRight(notes.String()+o.DeclaredCrumb+"\n"+o.Crumb+"\n"+o.SDKCrumb, "\n")
	return hooks.CheckResult{Findings: o.Findings, Errors: o.Errors, Err: o.Err, Crumb: crumb}
}

// hookGuards gives the hooks the checks service's guards.
type hookGuards struct{}

func (hookGuards) Judge(repo string, call hooks.Call, deadline time.Time) hooks.GuardResult {
	v := checkscmd.Service().Judge(repo, call.Event, checks.Call{Tool: call.Tool, Input: call.Input, Response: call.Response, Prompt: call.Prompt}, call.Session, deadline)
	r := hooks.GuardResult{Blocks: v.Blocks, Advice: v.Advice}
	for _, e := range v.Errors {
		r.Notes = append(r.Notes, "[cn] guard could not decide: "+e)
	}
	return r
}

// hookUserPack is SessionStart's user-pack step over the real process:
// its environment, the default HTTP client (which honours the session's
// proxy) and git.
type hookUserPack struct{}

func (hookUserPack) Prepare(repo string) string {
	r, ok := userpack.Prepare(repo, userpack.Env{Getenv: os.Getenv, Client: http.DefaultClient})
	if !ok {
		return ""
	}
	return r.Line()
}

// hookGrowth is the session-end capture over the real git.
type hookGrowth struct{}

func (hookGrowth) Capture(repo, session, transcript string, issue int, out io.Writer) breadcrumb.Outcome {
	req := capture.Request{Key: capture.Key{Issue: &issue}, Branch: capture.DefaultBranch, Dir: repo}
	if session != "" {
		req.Session = &session
	}
	if transcript != "" {
		req.Transcript = &transcript
	}
	return capture.Run(req, capture.FromProcess(), out, out).Outcome.Crumb()
}

// hookIndex gives the hooks the rules index writer.
type hookIndex struct{}

// Write refreshes the generated files where the member holds them and
// moves nothing: a hook leaves no CLAUDE.md edit in the session's tree.
func (hookIndex) Write(repo, engine string) (bool, error) {
	written, err := rulesindex.Refresh(repo, engine)
	return len(written) > 0, err
}

func (hookIndex) HasImport(repo string) bool { return rulesindex.ImportsHeldIndex(repo) }

func (hookIndex) HasRules(repo, engine string) bool {
	st, _, err := rulesindex.Check(repo, engine)
	return err == nil && st != rulesindex.Empty
}
