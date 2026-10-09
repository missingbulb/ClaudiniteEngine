package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/capture"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/prune"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/retention"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/hooks"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

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
	return crumbOf(capture.Run(req, capture.FromProcess(), out, out).Outcome)
}

func crumbOf(o capture.Outcome) breadcrumb.Outcome {
	switch o {
	case capture.OK:
		return breadcrumb.OK
	case capture.Skip:
		return breadcrumb.Skip
	}
	return breadcrumb.Error
}

func cmdGrowth(args []string, stdout, stderr io.Writer, start time.Time) error {
	if len(args) == 0 {
		return report.New(report.Usage, "growth takes capture or prune")
	}
	switch args[0] {
	case "capture":
		return growthCapture(args[1:], stdout, stderr, start)
	case "prune":
		return growthPrune(args[1:], stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown growth command %q", args[0]))
}

// growthCapture is `cn growth capture`: the merge-to-main skill's and a
// routine session's capture, and a person's.
func growthCapture(args []string, stdout, stderr io.Writer, start time.Time) error {
	wd, err := os.Getwd()
	if err != nil {
		return report.Wrap(report.IO, "growth capture", err)
	}
	crumb := func(o breadcrumb.Outcome) {
		fmt.Fprintln(stderr, breadcrumb.Line("growth", "capture", o, time.Since(start)))
	}
	req, ok := capture.ParseArgs(args, wd)
	if !ok {
		fmt.Fprintln(stderr, capture.UsageText)
		crumb(breadcrumb.Error)
		return report.Said(report.Usage)
	}
	res := capture.Run(req, capture.FromProcess(), stdout, stderr)
	crumb(crumbOf(res.Outcome))
	switch res.Code {
	case 0:
		return nil
	case 2:
		return report.Said(report.Usage)
	}
	return report.Said(report.IO)
}

// growthPrune is `cn growth prune`, the logs-prune task's code-work: the
// captures past the repo's retention_days, removed in one commit.
func growthPrune(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("growth prune", flag.ContinueOnError)
	branch := fs.String("branch", capture.DefaultBranch, "")
	repo := fs.String("repo", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	root, err := codeWorkRoot(*repo)
	if err != nil {
		return report.Wrap(report.IO, "growth prune", err)
	}
	declared, present, readable := retentionDeclared(root)
	if !readable {
		fmt.Fprintln(stdout, "this repo's claudinite-growth declaration could not be read for a retention — deleting nothing")
		return nil
	}
	days := retention.ResolveRetentionDays(declared, present)
	if days == nil {
		if n, ok := retention.Number(declared); ok {
			fmt.Fprintf(stdout, "retention_days is %s — capture-only by this repo's own choice, deleting nothing\n", jsjson.FormatNumber(n))
		} else {
			fmt.Fprintf(stdout, "retention_days is unreadable (%v, not a number) — deleting nothing\n", declared)
		}
		return nil
	}
	if !present {
		fmt.Fprintf(stdout, "retention_days is undeclared — using the %vd default\n", *days)
	}
	clock, err := world.Env(os.Getenv).Clock()
	if err != nil {
		return report.Wrap(report.Usage, "growth prune", err)
	}
	git := gitcmd.Repo{Dir: root, Token: os.Getenv("GITHUB_TOKEN")}
	if err := prune.Run(git, *branch, *days, clock.Now, stdout); err != nil {
		return report.Wrap(report.IO, "growth prune", err)
	}
	return nil
}

// codeWorkRoot is the repository a code-work command reads: --repo, else
// the executor's CLAUDINITE_REPO_ROOT (code-work runs in its task's
// folder), else the checkout around the working directory.
func codeWorkRoot(repo string) (string, error) {
	if repo == "" {
		repo = os.Getenv("CLAUDINITE_REPO_ROOT")
	}
	if repo == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		repo = wd
		if r, err := (gitcmd.Repo{Dir: wd}).Run("rev-parse", "--show-toplevel"); err == nil && r.Code == 0 {
			if top := strings.TrimSpace(r.Stdout); top != "" {
				repo = top
			}
		}
	}
	return filepath.Abs(repo)
}

// retentionDeclared is the claudinite-growth entry's retention_days as
// declared: present false where the entry says nothing, readable false
// where the declaration cannot be read or declares no such pack, or the
// value is not a finite number (an unknown is never a licence to delete).
func retentionDeclared(root string) (value any, present, readable bool) {
	path, f, err := settings.Find(root)
	if err != nil {
		return nil, false, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return nil, false, false
	}
	for _, e := range p.Entries {
		if e.Local || e.ID != hooks.GrowthPack {
			continue
		}
		v, ok := e.Config["retention_days"]
		if !ok {
			return nil, false, true
		}
		if _, num := retention.Number(v); !num {
			return v, true, false
		}
		return v, true, true
	}
	return nil, false, false
}
