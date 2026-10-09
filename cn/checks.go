package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/hooks"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// buildWait bounds a foreground wait for the checks binary, as in CI.
const buildWait = 10 * time.Minute

func checksService() checks.Service {
	exe, _ := os.Executable()
	return checks.Service{
		Build: build.Config{CacheRoot: paths.CacheRoot(), Engine: version.Version(), SDK: checksdk.Sources()},
		Exe:   exe,
	}
}

// foreground is the checks service for a command that waits on the build
// itself: its waits are reported on stderr, named by caller.
func foreground(caller string, stderr io.Writer) checks.Service {
	svc := checksService()
	svc.Caller = caller
	svc.Timing = func(line string) { fmt.Fprintln(stderr, line) }
	return svc
}

// hookChecks gives the hooks the checks service.
type hookChecks struct{}

func (hookChecks) Start(repo, session string) (string, error) {
	svc := checksService()
	svc.Session = session
	return svc.Start(repo)
}

func (hookChecks) Run(repo, event string, scope hooks.RunScope, wait time.Duration) hooks.CheckResult {
	var notes bytes.Buffer
	sel := declared.Selection{Tags: scope.Tags, Session: transcript.NewSession(scope.Transcript)}
	svc := checksService()
	svc.Session = scope.Session
	svc.Timing = func(line string) { notes.WriteString(line + "\n") }
	o := svc.RunAll(repo, event, sel, wait, false, &notes)
	crumb := strings.TrimRight(notes.String()+o.DeclaredCrumb+"\n"+o.Crumb+"\n"+o.SDKCrumb, "\n")
	return hooks.CheckResult{Findings: o.Findings, Errors: o.Errors, Err: o.Err, Crumb: crumb}
}

// hookGuards gives the hooks the checks service's guards.
type hookGuards struct{}

func (hookGuards) Judge(repo string, call hooks.Call, deadline time.Time) hooks.GuardResult {
	v := checksService().Judge(repo, call.Event, checks.Call{Tool: call.Tool, Input: call.Input, Response: call.Response, Prompt: call.Prompt}, call.Session, deadline)
	r := hooks.GuardResult{Blocks: v.Blocks, Advice: v.Advice}
	for _, e := range v.Errors {
		r.Notes = append(r.Notes, "[cn] guard could not decide: "+e)
	}
	return r
}

// allFindings runs the declared and coded checks in the foreground and
// turns a run that could not happen, or a check that failed, into a
// break. verbose adds the coded checks' SDK calls, by method, the checks
// a git fault skipped, and the tail of their stderr.
func allFindings(repo, event string, sel declared.Selection, verbose bool, stderr io.Writer) []findings.Finding {
	o := foreground(event, stderr).RunAll(repo, event, sel, buildWait, true, stderr)
	fmt.Fprintln(stderr, o.DeclaredCrumb)
	if !strings.Contains(o.Crumb, " ok ") {
		fmt.Fprintln(stderr, o.Crumb)
	}
	if o.SDKCrumb != "" {
		fmt.Fprintln(stderr, o.SDKCrumb)
	}
	if verbose {
		methods := make([]string, 0, len(o.Calls))
		for m := range o.Calls {
			methods = append(methods, m)
		}
		sort.Strings(methods)
		for _, m := range methods {
			fmt.Fprintf(stderr, "[cn] sdk %s %d\n", m, o.Calls[m])
		}
		for _, id := range o.Skipped {
			fmt.Fprintf(stderr, "[cn] check skipped after a git fault: %s\n", id)
		}
		for _, l := range strings.Split(strings.TrimRight(o.Stderr, "\n"), "\n") {
			if l != "" {
				fmt.Fprintf(stderr, "[cn] checks stderr: %s\n", l)
			}
		}
	}
	out := o.Findings
	if o.Err != nil {
		out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: ".claudinite/shared/packs", Sentence: o.Err.Error()})
	}
	for _, e := range o.Errors {
		out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: ".claudinite/shared/packs", Sentence: e})
	}
	return out
}

// printFindings prints the findings and, when there are any, the summary
// line.
func printFindings(w io.Writer, fs []findings.Finding, scope string) {
	findings.Print(w, fs)
	if line := declared.Summary(fs, scope); line != "" {
		fmt.Fprintln(w, line)
	}
}

func cmdCheck(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "world":
			return cmdCheckWorld(args[1:], stdout, stderr)
		case "build":
			return cmdCheckBuild(args[1:])
		case "list":
			return cmdCheckList(args[1:], stdout)
		case "sdk":
			return cmdCheckSDK(args[1:], stdout)
		}
	}
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	tag := fs.String("tag", "", "")
	pack := fs.String("pack", "", "")
	repo := fs.String("repo", ".", "")
	session := fs.String("transcript", "", "")
	verbose := fs.Bool("v", false, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *tag == "" && *pack == "" {
		return report.New(report.Usage, "check takes world, list, build, sdk, --tag TAG or --pack ID")
	}
	var tags []string
	if *tag != "" {
		tags = []string{*tag}
	}
	scope := *tag
	if scope == "" {
		scope = "pack " + *pack
	}
	// A coded listing that failed still lists the declared and built-in
	// checks; the run below reports the coded failure as a checks-run break.
	listed, err := foreground("check", stderr).ListAll(*repo, buildWait)
	if err != nil && listed == nil {
		return report.Wrap(report.Verify, "check", err)
	}
	for _, l := range listed {
		switch {
		case !selected(l, tags, *pack):
		case l.Kind == "judge":
			fmt.Fprintf(stdout, "judge %s (%s) runs only in its hook; check does not run it\n", l.Name(), strings.Join(l.Tags, ", "))
		default:
			fmt.Fprintf(stdout, "check %s (%s)\n", l.Name(), strings.Join(l.Tags, ", "))
		}
	}
	fs2 := allFindings(*repo, "check", declared.Selection{Tags: tags, Pack: *pack, Session: transcript.NewSession(*session)}, *verbose, stderr)
	printFindings(stdout, fs2, scope)
	if findings.AnyBreak(fs2) {
		return report.New(report.Verify, "check found a finding in "+*repo)
	}
	return nil
}

func selected(l checks.Listed, tags []string, pack string) bool {
	if pack != "" && l.Pack != pack {
		return false
	}
	for _, t := range tags {
		has := false
		for _, lt := range l.Tags {
			has = has || lt == t
		}
		if !has {
			return false
		}
	}
	return true
}

// cmdCheckList prints every check the repo declares, declared, built in
// coded and judge, sorted by id: name, kind, tags, on_fail and, where the
// check has one, since.
func cmdCheckList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("check list", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	listed, err := checksService().ListAll(*repo, buildWait)
	if err != nil {
		return report.Wrap(report.Verify, "check list", err)
	}
	counts := map[string]int{}
	for _, l := range listed {
		line := fmt.Sprintf("%s %s (%s) %s", l.Name(), l.Kind, strings.Join(l.Tags, ", "), l.OnFail)
		if l.Since != "" {
			line += " since " + l.Since
		}
		fmt.Fprintln(stdout, line)
		counts[l.Kind]++
	}
	fmt.Fprintf(stdout, "%d checks: %d declared, %d builtin, %d coded, %d judges\n", len(listed), counts["declared"], counts["builtin"], counts["coded"], counts["judge"])
	return nil
}

func cmdCheckBuild(args []string) error {
	fs := flag.NewFlagSet("check build", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	key := fs.String("key", "", "")
	wait := fs.Bool("wait", false, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if _, err := checksService().BuildNow(*repo, *key, *wait, buildWait); err != nil {
		return report.Wrap(report.IO, "check build", err)
	}
	return nil
}

// cmdCheckSDK writes the Go check SDK this cn builds pack checks against,
// with the go.mod stanza a pack repo's tests replace to, so those tests
// resolve the SDK offline.
func cmdCheckSDK(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("check sdk", flag.ContinueOnError)
	out := fs.String("out", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *out == "" {
		return report.New(report.Usage, "check sdk takes --out DIR")
	}
	dir, err := filepath.Abs(*out)
	if err != nil {
		return report.Wrap(report.IO, "check sdk", err)
	}
	if err := build.WriteSDK(dir, checksdk.Sources()); err != nil {
		return report.Wrap(report.IO, "check sdk", err)
	}
	fmt.Fprintf(stdout, "wrote the check SDK to %s; its go.mod.stanza is the lines a test module adds\n", dir)
	return nil
}
