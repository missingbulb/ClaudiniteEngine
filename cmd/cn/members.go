package main

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/world"
	"github.com/missingbulb/ClaudiniteEngine/launcher"
	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/verify"
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
	return verify.Verify(verify.Input{Repo: repo, Launcher: launcher.Script, Shipped: launcher.Shipped()})
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

func cmdCheck(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "world" {
		return report.New(report.Usage, "check takes world")
	}
	fs := flag.NewFlagSet("check world", flag.ContinueOnError)
	author := fs.String("pr-author", "", "")
	base := fs.String("base-ref", "", "")
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	if *author == "" || *base == "" {
		return report.New(report.Usage, "check world needs --pr-author and --base-ref")
	}
	code := world.Run(stdout, world.Input{
		Repo: *repo, PRAuthor: *author, BaseRef: *base, Git: gitcmd.Repo{Dir: *repo},
		CheckPin: checkPin, Findings: verifyFindings(*repo),
	})
	if code != 0 {
		return report.New(report.Verify, "check world refused")
	}
	return nil
}

// checkPin verifies a new pin as the updater would before proposing it,
// and refuses a version npm marks deprecated, held or revoked.
func checkPin(e settings.Engine) error {
	reg, err := npmreg.FromEnv()
	if err != nil {
		return err
	}
	p, err := reg.Packument(e.Package)
	if err != nil {
		return err
	}
	if k, reason := update.StatesFromPackument(p).Of(e.Version); k != "" {
		return fmt.Errorf("%s %s is %s: %s", e.Package, e.Version, k, reason)
	}
	if v, ok := p.Versions[e.Version]; ok && v.Deprecated != "" {
		return fmt.Errorf("%s %s is deprecated on npm: %s", e.Package, e.Version, v.Deprecated)
	}
	roots, err := license.Roots()
	if err != nil {
		return err
	}
	got, err := update.Fetch(update.FetchInput{Registry: reg, Package: e.Package, Version: e.Version, Packument: p, Roots: roots,
		CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now()})
	if err != nil {
		return err
	}
	if got.Integrity != e.Manifest {
		return fmt.Errorf("engine.manifest %s is not the SHA-512 of %s %s's manifest.json (%s)", e.Manifest, e.Package, e.Version, got.Integrity)
	}
	return nil
}
