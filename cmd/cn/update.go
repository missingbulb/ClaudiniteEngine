package main

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/trust"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
	"github.com/missingbulb/ClaudiniteEngine/tasks/execute"
)

// childTimeout bounds each run of a candidate binary.
const childTimeout = 2 * time.Minute

// updateDeps builds the updater's real dependencies. The job token is read
// once and removed from the environment, so no child process inherits it;
// only the git children that talk to the remote are handed it.
func updateDeps(repo string, stdout io.Writer) (update.Deps, error) {
	token := os.Getenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GITHUB_TOKEN")
	return updateDepsWith(repo, token, stdout)
}

// updateDepsWith builds them over a token the caller already read.
func updateDepsWith(repo, token string, stdout io.Writer) (update.Deps, error) {
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
	roots, err := trust.Roots()
	if err != nil {
		return update.Deps{}, report.Wrap(report.Internal, "update", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return update.Deps{}, report.Wrap(report.Internal, "update", err)
	}
	return update.Deps{GitHub: gh, Registry: reg, Git: gitcmd.Repo{Dir: repo, Token: token}, Roots: roots,
		CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now, Sleep: time.Sleep,
		Repo: repo, Out: stdout, Timeout: childTimeout, Exe: exe}, nil
}

// packReader reads the pack indexes from the pack sources repo's settings
// name, logging which answered to out. close removes every branch's clone.
func packReader(repo string, roots []ed25519.PublicKey, out io.Writer) (*packs.Reader, func(), error) {
	list, err := repoSources(repo)
	if err != nil {
		return nil, nil, err
	}
	srcs, closer := packs.SourcesFor(list, &http.Client{Timeout: time.Minute})
	return &packs.Reader{Sources: srcs, Roots: roots, Now: time.Now, Log: out}, closer, nil
}

// repoSources is packs.sources from repo's settings: none where the repo
// holds no settings file yet, an adoption, which reads the shelf.
func repoSources(repo string) ([]string, error) {
	held := false
	for _, f := range settings.Formats {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(settings.RelPath(f)))); err == nil {
			held = true
		}
	}
	if !held {
		return nil, nil
	}
	path, f, err := settings.Find(repo)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return nil, err
	}
	return p.Sources, nil
}

// cmdUpdateDecide answers one of the update's decision cores over a
// fixture, printing JSON: the parity harness's update face and a person's
// reproduction tool, never a CI path.
func cmdUpdateDecide(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "update decide needs a core: plan, gap, delivery, applystage, terminal, convergescope or pulltext")
	}
	fs := flag.NewFlagSet("update decide", flag.ContinueOnError)
	world := fs.String("world", "", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	if *world == "" {
		return report.New(report.Usage, "update decide needs --world FILE")
	}
	raw, err := os.ReadFile(*world)
	if err != nil {
		return report.Wrap(report.IO, "update decide", err)
	}
	answer, err := update.Decide(args[0], raw)
	if err != nil {
		return report.New(report.Usage, err.Error())
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(answer)
}

func cmdUpdate(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "decide" {
		return cmdUpdateDecide(args[1:], stdout)
	}
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
	reader, closeReader, err := packReader(*repo, d.Roots, stdout)
	if err != nil {
		return report.Wrap(report.IO, "update "+args[0], err)
	}
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

// runUpdateTask is the engine/update task's code-work, run in the
// executor's process: the engine update, then the packs update, from the
// default branch, each verdict said on the item's close. The update's pull
// requests are its own, landed by cn update land, so it delivers none.
func runUpdateTask(repo, token, branch string, out io.Writer) execute.CodeWorkResult {
	d, err := updateDepsWith(repo, token, out)
	if err != nil {
		return execute.CodeWorkResult{Why: "engine/update: " + err.Error()}
	}
	if cur, err := d.Git.CurrentBranch(); err != nil || cur != branch {
		if err := d.Git.Checkout(branch); err != nil {
			return execute.CodeWorkResult{Why: "engine/update: could not check out " + branch, Detail: err.Error()}
		}
	}
	reader, closeReader, err := packReader(repo, d.Roots, out)
	if err != nil {
		return execute.CodeWorkResult{Why: "engine/update: the pack sources do not read", Detail: err.Error()}
	}
	defer closeReader()
	d.Packs = reader
	var said []string
	for _, step := range []struct {
		name string
		run  func(update.Deps, update.Options) (string, error)
	}{{"cn update engine", update.Engine}, {"cn update packs", update.Packs}} {
		verdict, err := step.run(d, update.Options{})
		if err != nil {
			return execute.CodeWorkResult{Why: "engine/update: " + step.name + " failed", Detail: err.Error(), Said: said}
		}
		fmt.Fprintln(out, step.name+": "+verdict)
		said = append(said, step.name+": "+verdict)
	}
	return execute.CodeWorkResult{OK: true, Said: said}
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
