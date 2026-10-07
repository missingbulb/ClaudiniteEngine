package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks"
	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/checks/world"
	"github.com/missingbulb/ClaudiniteEngine/launcher"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/verify"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/trust"
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

func verifyFindings(repo string, stderr io.Writer) []findings.Finding {
	return verify.Verify(verify.Input{Repo: repo, Launcher: launcher.Script, Shipped: launcher.Shipped(), Declared: declaredChecks(stderr)})
}

// declaredChecks answers verify's questions about the active packs'
// checks: every declared, built-in and coded check by both names a rule
// may use, and the descriptors that did not load. The coded checks come
// only from a binary already built, never a build: without one the list
// is Partial, and stderr says once what went unjudged.
func declaredChecks(stderr io.Writer) func(string) verify.DeclaredChecks {
	said := false
	return func(repo string) verify.DeclaredChecks {
		set, err := checksService().LoadSet(repo)
		if err != nil {
			return verify.DeclaredChecks{}
		}
		out := verify.DeclaredChecks{}
		listed, err := checksService().ListAllBuilt(repo)
		if err != nil {
			out.Partial = true
			if errors.Is(err, checks.ErrNotBuilt) && !said {
				said = true
				fmt.Fprintln(stderr, "[cn] verify: the coded checks are not built, so rule keys in the settings were not judged against them; cn check world builds them")
			}
		}
		for _, l := range listed {
			out.IDs = append(out.IDs, declared.Names(l.Pack, l.ID)...)
		}
		for _, le := range set.LoadErrors {
			out.Faults = append(out.Faults, verify.DescriptorFault{Path: le.Path, Sentence: le.Err.Error(), Duplicate: errors.Is(le.Err, descriptor.ErrDuplicate)})
		}
		return out
	}
}

func cmdVerify(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	fs2 := verifyFindings(*repo, stderr)
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
	// The checks run first: their foreground build is the binary verify
	// then lists the coded checks from.
	ran := allFindings(*repo, "world", declared.Selection{Tags: []string{"world"}}, false, stderr)
	all := append(verifyFindings(*repo, stderr), ran...)
	full := repoFullName(*repo)
	in := world.Input{Repo: *repo, PRAuthor: *author, BaseRef: *base, Git: g, CheckPin: checkPin, Findings: all,
		Workflows: workflows.Names, ExpectedWorkflow: func(name string, base []byte) ([]byte, error) {
			return workflows.Expected(name, base, full)
		}}
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
	roots, err := trust.Roots()
	if err != nil {
		return err
	}
	return update.CheckPin(update.Deps{Registry: reg, Roots: roots, CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now}, e)
}
