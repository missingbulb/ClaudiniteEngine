package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// childTimeout bounds each run of a candidate binary.
const childTimeout = 2 * time.Minute

// updateDeps builds the updater's real dependencies. The job token is read
// once and removed from the environment, so no child process sees it.
func updateDeps(repo string, stdout io.Writer) (update.Deps, error) {
	token := os.Getenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GITHUB_TOKEN")
	if token == "" {
		return update.Deps{}, report.New(report.IO, "update needs GITHUB_TOKEN, the workflow job's token")
	}
	gh, err := githubapi.FromEnv(token)
	if err != nil {
		return update.Deps{}, report.Wrap(report.IO, "update", err)
	}
	reg, err := npmreg.FromEnv()
	if err != nil {
		return update.Deps{}, report.Wrap(report.IO, "update", err)
	}
	roots, err := license.Roots()
	if err != nil {
		return update.Deps{}, report.Wrap(report.Internal, "update", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return update.Deps{}, report.Wrap(report.Internal, "update", err)
	}
	return update.Deps{GitHub: gh, Registry: reg, Git: gitcmd.Repo{Dir: repo}, Roots: roots,
		CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now,
		Repo: repo, Out: stdout, Timeout: childTimeout, Exe: exe}, nil
}

// packReader reads the pack indexes from the CDN and the vendored branch,
// logging which answered to out. close removes the branch's clone.
func packReader(roots []ed25519.PublicKey, out io.Writer) (*packs.Reader, func()) {
	cdn, branch := packs.Sources(&http.Client{Timeout: time.Minute})
	return &packs.Reader{Sources: []packs.Source{cdn, branch}, Roots: roots, Now: time.Now, Log: out}, branch.Close
}

func cmdUpdate(args []string, stdout io.Writer) error {
	if len(args) == 0 || (args[0] != "engine" && args[0] != "packs" && args[0] != "land") {
		return report.New(report.Usage, "update takes engine, packs or land")
	}
	fs := flag.NewFlagSet("update "+args[0], flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	force := fs.Bool("force", false, "")
	pr := fs.Int("pr", 0, "")
	sha := fs.String("sha", "", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	if args[0] == "land" {
		if *pr <= 0 {
			return report.New(report.Usage, "update land needs --pr N")
		}
		if *sha == "" {
			return report.New(report.Usage, "update land needs --sha, the head CI ran on")
		}
	}
	d, err := updateDeps(*repo, stdout)
	if err != nil {
		return err
	}
	reader, closeReader := packReader(d.Roots, stdout)
	defer closeReader()
	d.Packs = reader
	var verdict string
	switch args[0] {
	case "land":
		verdict, err = update.Land(d, *pr, *sha)
	case "packs":
		verdict, err = update.Packs(d, update.Options{Force: *force})
	default:
		verdict, err = update.Engine(d, update.Options{Force: *force})
	}
	if err != nil {
		return report.Wrap(report.IO, "update "+args[0], err)
	}
	fmt.Fprintln(stdout, verdict)
	return nil
}

func cmdWorkflows(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "diff" {
		return report.New(report.Usage, "workflows takes diff")
	}
	fs := flag.NewFlagSet("workflows diff", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	d, err := workflows.Diff(*repo)
	if err != nil {
		return report.Wrap(report.IO, "workflows diff", err)
	}
	fmt.Fprint(stdout, d)
	return nil
}
