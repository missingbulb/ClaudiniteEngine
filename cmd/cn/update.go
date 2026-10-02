package main

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// childTimeout bounds each run of a candidate binary.
const childTimeout = 2 * time.Minute

// updateDeps builds the updater's real dependencies. The job token is read
// once and removed from the environment, so no child process inherits it;
// only the git children that talk to the remote are handed it.
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
	return update.Deps{GitHub: gh, Registry: reg, Git: gitcmd.Repo{Dir: repo, Token: token}, Roots: roots,
		CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now,
		Repo: repo, Out: stdout, Timeout: childTimeout, Exe: exe, Key: actionsKey(roots, stdout)}, nil
}

// actionsKey requests the run's Actions key at most once per process and
// hands the updater what it reads of it.
// runKey is the process's one Actions key request, shared by every caller
// that needs the run's key.
var runKey struct {
	once sync.Once
	key  *license.ActionsOnce
}

func sharedActionsKey(roots []ed25519.PublicKey, log io.Writer) *license.ActionsOnce {
	runKey.once.Do(func() {
		runKey.key = &license.ActionsOnce{Request: func() license.ActionsResult {
			start := time.Now()
			w, err := licenseapi.FromEnv()
			var r license.ActionsResult
			if err != nil {
				r = license.ActionsResult{Cause: license.CauseServerUnreachable, Detail: err.Error()}
			} else {
				r = license.RequestActions(w, &http.Client{Timeout: 10 * time.Second}, os.Getenv, roots, time.Now, version.Version())
			}
			outcome := breadcrumb.OK
			if r.Key == nil {
				outcome = breadcrumb.Error
			}
			fmt.Fprintln(log, breadcrumb.Line("license", "request-actions", outcome, time.Since(start)))
			if r.Key != nil {
				fmt.Fprintf(log, "license key: %s plan, %s\n", r.Key.Plan, r.Key.State)
			}
			return r
		}}
	})
	return runKey.key
}

func actionsKey(roots []ed25519.PublicKey, log io.Writer) func() update.KeyResult {
	shared := sharedActionsKey(roots, log)
	return func() update.KeyResult {
		r := shared.Key()
		if r.Key == nil {
			return update.KeyResult{Cause: string(r.Cause), Detail: r.Detail, Link: r.Link}
		}
		k := r.Key
		notice := strings.TrimPrefix(license.NoticeFor(k, "", "", ""), "[cn] license degraded: ")
		return update.KeyResult{Key: &update.LicenseKey{Plan: string(k.Plan), State: k.State, Notice: notice,
			IssuedAt: time.Unix(k.Iat, 0), Held: k.Release.Held, Revoked: k.Release.Revoked, SecurityFixes: k.Release.SecurityFixes,
			SerialFloor: k.Release.PackIndexSerial, PackKeys: k.Release.PackKeys}}
	}
}

// packReader reads the pack indexes from the CDN and the vendored branch,
// logging which answered to out. close removes the branch's clone.
func packReader(roots []ed25519.PublicKey, out io.Writer) (*packs.Reader, func()) {
	cdn, branch := packs.Sources(&http.Client{Timeout: time.Minute})
	return &packs.Reader{Sources: []packs.Source{cdn, branch}, Roots: roots, Now: time.Now, Log: out}, branch.Close
}

// keyedReader applies the run's key to the pack reader at the first index
// read, which the updater makes only after the key was granted.
type keyedReader struct {
	*packs.Reader
	key  func() update.KeyResult
	once sync.Once
}

func (r *keyedReader) VerifiedIndex(id string) (packs.Verified, error) {
	r.once.Do(func() {
		if k := r.key().Key; k != nil {
			r.SerialFloor, r.AcceptedKeys = k.SerialFloor, k.PackKeys
		}
	})
	return r.Reader.VerifiedIndex(id)
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
	reader, closeReader := packReader(d.Roots, stdout)
	defer closeReader()
	if args[0] == "land" {
		// claudinite-ci, where land runs, is not a workflow the license
		// server issues keys to; landing re-reads npm's states instead.
		d.Key = func() update.KeyResult { return update.KeyResult{Cause: "not-requested"} }
	}
	d.Packs = &keyedReader{Reader: reader, key: d.Key}
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
