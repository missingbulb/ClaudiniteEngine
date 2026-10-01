package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks"
	"github.com/missingbulb/ClaudiniteEngine/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/hooks"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// buildWait bounds a foreground wait for the checks binary, as in CI.
const buildWait = 10 * time.Minute

func checksService() checks.Service {
	exe, _ := os.Executable()
	return checks.Service{
		Build: build.Config{CacheRoot: paths.CacheRoot(), Engine: version.Version(), SDK: map[string][]byte{"checksdk.go": checksdk.Source}},
		Exe:   exe,
	}
}

// hookChecks gives the hooks the checks service.
type hookChecks struct{}

func (hookChecks) Start(repo string) error { return checksService().Start(repo) }

func (hookChecks) Run(repo, event string, tags []string, wait time.Duration) hooks.CheckResult {
	var notes bytes.Buffer
	o := checksService().RunAll(repo, event, tags, "", wait, false, &notes)
	crumb := strings.TrimRight(notes.String()+o.DeclaredCrumb+"\n"+o.Crumb, "\n")
	return hooks.CheckResult{Findings: o.Findings, Errors: o.Errors, Err: o.Err, Crumb: crumb}
}

// allFindings runs the declared and coded checks in the foreground and
// turns a run that could not happen, or a check that failed, into a
// break.
func allFindings(repo, event string, tags []string, pack string, stderr io.Writer) []findings.Finding {
	o := checksService().RunAll(repo, event, tags, pack, buildWait, true, stderr)
	fmt.Fprintln(stderr, o.DeclaredCrumb)
	if !strings.Contains(o.Crumb, " ok ") {
		fmt.Fprintln(stderr, o.Crumb)
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
		}
	}
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	tag := fs.String("tag", "", "")
	pack := fs.String("pack", "", "")
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *tag == "" && *pack == "" {
		return report.New(report.Usage, "check takes world, list, build, --tag TAG or --pack ID")
	}
	var tags []string
	if *tag != "" {
		tags = []string{*tag}
	}
	scope := *tag
	if scope == "" {
		scope = "pack " + *pack
	}
	listed, err := checksService().ListAll(*repo, buildWait)
	if err != nil {
		return report.Wrap(report.Verify, "check", err)
	}
	for _, l := range listed {
		if selected(l, tags, *pack) {
			fmt.Fprintf(stdout, "check %s (%s)\n", l.Name(), strings.Join(l.Tags, ", "))
		}
	}
	fs2 := allFindings(*repo, "check", tags, *pack, stderr)
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
// and coded, sorted by id: name, kind, tags, on_fail.
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
		fmt.Fprintf(stdout, "%s %s (%s) %s\n", l.Name(), l.Kind, strings.Join(l.Tags, ", "), l.OnFail)
		counts[l.Kind]++
	}
	fmt.Fprintf(stdout, "%d checks: %d declared, %d builtin, %d coded\n", len(listed), counts["declared"], counts["builtin"], counts["coded"])
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
