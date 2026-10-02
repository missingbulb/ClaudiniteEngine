package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/checks/world"
	"github.com/missingbulb/ClaudiniteEngine/launcher"
	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/verify"
	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// flags parses args against fs, mapping any problem to a usage error.
func flags(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return report.Wrap(report.Usage, fs.Name(), err)
	}
	if fs.NArg() != 0 {
		return report.New(report.Usage, fs.Name()+" takes no positional arguments")
	}
	return nil
}

func verifyFindings(repo string) []findings.Finding {
	return verify.Verify(verify.Input{Repo: repo, Launcher: launcher.Script, Shipped: launcher.Shipped(), Declared: declaredChecks})
}

// declaredChecks answers verify's questions about the active packs'
// declared checks from the declared-checks loader.
func declaredChecks(repo string) verify.DeclaredChecks {
	set, err := declared.LoadSet(repo, version.Version())
	if err != nil {
		return verify.DeclaredChecks{}
	}
	out := verify.DeclaredChecks{IDs: set.IDs()}
	for _, le := range set.LoadErrors {
		out.Faults = append(out.Faults, verify.DescriptorFault{Path: le.Path, Sentence: le.Err.Error(), Duplicate: errors.Is(le.Err, descriptor.ErrDuplicate)})
	}
	return out
}

func cmdVerify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	fs2 := verifyFindings(*repo)
	findings.Print(stdout, fs2)
	if findings.AnyBreak(fs2) {
		return report.New(report.Verify, "verify found a break in "+*repo)
	}
	return nil
}

func cmdCheckWorld(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("check world", flag.ContinueOnError)
	author := fs.String("pr-author", "", "")
	base := fs.String("base-ref", "", "")
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if _, _, err := settings.Find(*repo); err != nil {
		// Not a member: there is nothing to judge.
		return nil
	}
	g := gitcmd.Repo{Dir: *repo}
	if *author != "" && *base == "" {
		*base = g.BaseRef()
	}
	all := append(verifyFindings(*repo), allFindings(*repo, "world", declared.Selection{Tags: []string{"world"}}, stderr)...)
	in := world.Input{Repo: *repo, PRAuthor: *author, BaseRef: *base, Git: g, CheckPin: checkPin, Findings: all}
	var code int
	if *author == "" || *base == "" {
		code = world.Report(stdout, in)
	} else {
		code = world.Run(stdout, in)
	}
	if line := declared.Summary(all, "world"); line != "" {
		fmt.Fprintln(stdout, line)
	}
	if code != 0 {
		return report.New(report.Verify, "check world refused")
	}
	return nil
}

// checkPin is the updater's own pin check, with this binary's roots.
func checkPin(e settings.Engine) error {
	reg, err := npmreg.FromEnv()
	if err != nil {
		return err
	}
	roots, err := license.Roots()
	if err != nil {
		return err
	}
	return update.CheckPin(update.Deps{Registry: reg, Roots: roots, CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now}, e)
}
