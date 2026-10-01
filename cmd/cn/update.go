package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
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
	return update.Deps{GitHub: gh, Registry: reg, Git: gitcmd.Repo{Dir: repo}, Roots: roots,
		CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now,
		Repo: repo, Out: stdout, Timeout: childTimeout}, nil
}

func cmdUpdate(args []string, stdout io.Writer) error {
	if len(args) == 0 || (args[0] != "engine" && args[0] != "land") {
		return report.New(report.Usage, "update takes engine or land")
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
	var verdict string
	if args[0] == "land" {
		verdict, err = update.Land(d, *pr, *sha)
	} else {
		verdict, err = update.Engine(d, update.Options{Force: *force})
	}
	if err != nil {
		return report.Wrap(report.IO, "update "+args[0], err)
	}
	fmt.Fprintln(stdout, verdict)
	return nil
}
