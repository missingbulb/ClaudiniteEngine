package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks"
	"github.com/missingbulb/ClaudiniteEngine/checks/build"
	checksrun "github.com/missingbulb/ClaudiniteEngine/checks/run"
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
	res, crumb := checksService().Run(repo, event, tags, "", wait, false)
	return hooks.CheckResult{Findings: res.Shared(), Errors: res.Errors, Err: res.Err, Crumb: crumb}
}

// codedFindings runs the coded checks in the foreground and turns a run
// that could not happen, or a check that failed, into a break.
func codedFindings(repo, event string, tags []string, pack string, stderr io.Writer) []findings.Finding {
	res, crumb := checksService().Run(repo, event, tags, pack, buildWait, true)
	if !strings.Contains(crumb, " ok ") {
		fmt.Fprintln(stderr, crumb)
	}
	out := res.Shared()
	if res.Err != nil {
		out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: ".claudinite/shared/packs", Sentence: res.Err.Error()})
	}
	for _, e := range res.Errors {
		out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: ".claudinite/shared/packs", Sentence: e})
	}
	return out
}

func cmdCheck(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "world" {
		return cmdCheckWorld(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "build" {
		return cmdCheckBuild(args[1:])
	}
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	tag := fs.String("tag", "", "")
	pack := fs.String("pack", "", "")
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *tag == "" && *pack == "" {
		return report.New(report.Usage, "check takes world, build, --tag TAG or --pack ID")
	}
	var tags []string
	if *tag != "" {
		tags = []string{*tag}
	}
	svc := checksService()
	listed, err := svc.List(*repo, buildWait)
	if err != nil {
		return report.Wrap(report.Verify, "check", err)
	}
	for _, l := range matching(listed, tags, *pack) {
		fmt.Fprintf(stdout, "check %s (%s)\n", l.Check, strings.Join(l.Tags, ", "))
	}
	fs2 := codedFindings(*repo, "check", tags, *pack, stderr)
	findings.Print(stdout, fs2)
	if findings.AnyBreak(fs2) {
		return report.New(report.Verify, "check found a finding in "+*repo)
	}
	return nil
}

func matching(listed []checksrun.Listed, tags []string, pack string) []checksrun.Listed {
	var out []checksrun.Listed
	for _, l := range listed {
		if pack != "" && !strings.HasPrefix(l.Check, pack+"/") {
			continue
		}
		ok := true
		for _, t := range tags {
			has := false
			for _, lt := range l.Tags {
				has = has || lt == t
			}
			ok = ok && has
		}
		if ok {
			out = append(out, l)
		}
	}
	return out
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
