// Command cn is the Claudinite engine.
package main

import (
	"fmt"
	"github.com/missingbulb/ClaudiniteEngine/checksdk"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/hooks"
	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/selftest"
	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const usage = `usage: cn <command> [arguments]

commands:
  hook <event>   answer a Claude Code hook: session-start, pre-tool-use,
                 post-tool-use, user-prompt-submit, stop, session-end
  version        print the engine version
  version --day  print today's <day> version part (UTC), for release tooling
  selftest       check this machine, and with --repo DIR that member, can run the engine
  verify [--repo DIR]
                 check a member's files against this engine version;
                 exit 1 on a break
  check world --pr-author LOGIN --base-ref REF [--repo DIR]
                 the CI gate: the pin and launcher guard, verify, then the
                 declared packs' world-tagged checks
  check --tag TAG | --pack ID [--transcript PATH] [--repo DIR]
                 run a slice of the declared packs' checks; exit 1 on a
                 finding; --transcript is the session the work checks read
  check build [--wait] [--key KEY] [--repo DIR]
                 build the repo's checks binary (session-start starts it)
  update engine [--force] [--repo DIR]
                 propose or land the newest allowed engine version as a
                 pin-only PR; needs GITHUB_TOKEN; ends on its verdict line
  update packs [--force] [--repo DIR]
                 propose or land the declared packs' newest allowed
                 versions as a pack-only PR, once this repo's check world
                 passes over it; needs GITHUB_TOKEN
  update land --pr N --sha SHA [--repo DIR]
                 merge update PR N (engine or packs), whose CI passed on SHA
  init --packs ID[,ID] [--answer PACK/Q=TEXT]... [--channel stable|canary]
                 [--package PKG] [--repo DIR]
                 adopt a repo: pin the newest allowed engine, write the
                 member files, vendor the packs and what they require,
                 seed and stamp for them; ends on the QUESTIONS, HANDOVER
                 and NEXT blocks
  init --from-node [--channel stable|canary] [--package PKG] [--repo DIR]
                 move a Node member: pin, import .claudinite-settings.json,
                 vendor its canon packs again, replace the Node hooks and
                 write the workflows; deletes no member file
  adopt ID[,ID] [--answer PACK/Q=TEXT]... [--repo DIR]
                 declare and vendor more packs on an adopted repo, every
                 id resolved before any write; ends as init does
  settings answer PACK/QUESTION TEXT [--repo DIR]
                 record a person's answer to a pack's adoption question
  settings import [--from FILE] [--stdout] [--repo DIR]
                 read the Node engine's .claudinite-settings.json into the
                 pinned .claudinite/settings.* as its packs and checks
                 blocks, one report line per key; --stdout prints the
                 blocks as YAML instead; exit 1 when a key is refused
  login [--logout] [--force]
                 log in to the Claudinite App on this machine (GitHub's
                 device flow), so desktop sessions request their key
                 directly
  license status [--session ID] [--repo DIR]
                 the session's license state and the surfaces it turns off
  license request --session ID [--nonce N] [--repo DIR]
                 the session's key request: with --nonce, the background
                 request SessionStart starts; without, a foreground one
  rules-index [--check] [--repo DIR]
                 write the import index of the active packs' prose that
                 CLAUDE.md imports, and the flat task and dashboard
                 declarations beside it; --check exits 1 when it is stale
  tasks list [--repo DIR]
                 every task the active packs and the engine contribute
  tasks flat [--write|--check] [--repo DIR]
                 the flat task and dashboard declarations: print, write,
                 or exit 1 naming each file that is stale
  schedule run [--dry-run] [--wake IDS] [--repo DIR]
                 the scheduler run: repair, ask every scheduled task,
                 ready, adopt, reclaim; publishes the drain gate; needs
                 GITHUB_TOKEN
  schedule drain
                 dispatch the executor workflow on the default branch
  schedule report-failure [--title T]
                 file, or comment on, the one workflow-failure issue
  work create <pack>/<task> [--urgent] [--context T] [--not-before ISO]
                 [--blocked-by #N,#M] [--qualifier T] [--supersedes #N]
                 file a work item by hand; an unqualified item for a
                 scheduled task is refused
  work wake #N [--urgent]
                 clear an item's wait and return it to the queue
  work converge --issue N --outcome O --summary T [--pr N] --repo R
                 --item-file PATH
                 print the transition a routine session performs to
                 converge its item; refuses an item it does not hold
  work record-exec <pack>/<task> <slot> <success|failed>
                 print one execution record
  work validate --issue N --nonce X --item-file PATH --comments-file PATH
                 [--request-file PATH] [--repo DIR]
                 a routine session's entry gate: the nonce and the grant
  execute loop [--repo DIR]
                 the executor: claim, re-evaluate, run and converge every
                 ready item; needs GITHUB_TOKEN
  execute continue
                 dispatch the next executor run after one died, or past
                 the chain's depth report it
  workflows diff [--repo DIR]
                 the patch that brings a member's workflows to this
                 version's templates; empty when they match
`

// secretScanPlant is set only by the secret scan's own test build, to prove
// the scan finds a planted string; it is empty in every real build.
var secretScanPlant string

// runHook is a variable so a test can make a hook panic.
var runHook = hooks.Handler{Checks: hookChecks{}, Guards: hookGuards{}, License: hookLicense{}, Index: hookIndex{}}.Run

func main() {
	if len(os.Args) == 3 && os.Args[1] == "hook" && perCallEvents[os.Args[2]] {
		packset.Memoize()
	}
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// perCallEvents are the hooks a session runs on every call: each is one
// short process over a tree it does not write, which derives the packs
// and their triggers once for every capability that asks.
var perCallEvents = map[string]bool{"pre-tool-use": true, "post-tool-use": true, "user-prompt-submit": true}

// run dispatches one command. It holds the binary's one recover(): a panic
// anywhere below becomes a crash file, one stderr line and a crash
// breadcrumb, and exits 1 for a command, 0 for a hook.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	start := time.Now()
	if len(args) == 0 {
		return usageExit(stderr, report.New(report.Usage, "no command"))
	}
	isHook := args[0] == "hook"
	capability, event := "lifecycle", args[0]
	if isHook {
		capability, event = "hooks", ""
		if len(args) > 1 {
			event = args[1]
		}
	}
	_ = report.Prune(report.CrashDir(paths.CacheRoot()), start)

	defer func() {
		r := recover()
		if r == nil {
			return
		}
		code = crashed(args, r, debug.Stack(), stderr, isHook)
		fmt.Fprintln(stderr, breadcrumb.Line(capability, event, breadcrumb.Crash, time.Since(start)))
	}()

	err := dispatch(args, stdin, stdout, stderr, start)
	if report.CodeOf(err) == report.Usage {
		return usageExit(stderr, err)
	}
	return report.Exit(stderr, err, isHook)
}

func usageExit(stderr io.Writer, err error) int {
	code := report.Exit(stderr, err, false)
	fmt.Fprint(stderr, usage)
	return code
}

func dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer, start time.Time) error {
	switch args[0] {
	case "hook":
		if len(args) != 2 {
			return report.New(report.Usage, "hook takes exactly one event")
		}
		return runHook(args[1], stdin, stdout, stderr, start)
	case "version":
		if len(args) == 2 && args[1] == "--day" {
			fmt.Fprintln(stdout, version.Today(time.Now()))
			return nil
		}
		if len(args) == 2 && args[1] == "--floor" {
			fmt.Fprintln(stdout, checksdk.EngineFloor)
			return nil
		}
		if len(args) != 1 {
			return report.New(report.Usage, "version takes no arguments but --day or --floor")
		}
		lifecycle.PrintVersion(stdout)
		if secretScanPlant != "" {
			fmt.Fprintln(stdout, secretScanPlant)
		}
		return nil
	case "selftest":
		if len(args) == 2 && args[1] == "--panic" {
			panic("selftest --panic: deliberate crash for the crash-reporting tests")
		}
		in := selftest.Input{Version: version.Version(), Platform: version.Platform(), CacheRoot: paths.CacheRoot(), Now: time.Now(), HookEvents: hooks.Events}
		switch {
		case len(args) == 3 && args[1] == "--repo":
			in.Repo = args[2]
		case len(args) != 1:
			return report.New(report.Usage, "selftest takes no arguments but --repo DIR")
		}
		roots, err := license.Roots()
		in.RootsErr = err
		for _, r := range roots {
			in.RootIDs = append(in.RootIDs, sign.KeyID(r))
		}
		if selftest.Selftest(stdout, in) != 0 {
			return report.New(report.IO, "selftest failed")
		}
		return nil
	case "verify":
		return cmdVerify(args[1:], stdout, stderr)
	case "check":
		return cmdCheck(args[1:], stdout, stderr)
	case "update":
		return cmdUpdate(args[1:], stdout)
	case "workflows":
		return cmdWorkflows(args[1:], stdout)
	case "init":
		return cmdInit(args[1:], stdout, stderr)
	case "adopt":
		return cmdAdopt(args[1:], stdout)
	case "settings":
		return cmdSettings(args[1:], stdout)
	case "login":
		return cmdLogin(args[1:], stdout)
	case "license":
		return cmdLicense(args[1:], stdout, stderr)
	case "rules-index":
		return cmdRulesIndex(args[1:], stdout)
	case "tasks":
		return cmdTasks(args[1:], stdout)
	case "schedule":
		return cmdSchedule(args[1:], stdout)
	case "work":
		return cmdWork(args[1:], stdout)
	case "execute":
		return cmdExecute(args[1:], stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown command %q", args[0]))
}

func crashed(args []string, value any, stack []byte, stderr io.Writer, isHook bool) int {
	path, err := report.WriteCrash(report.CrashDir(paths.CacheRoot()), report.Crash{
		Version: version.Version(), Platform: version.Platform(), Args: args,
		Value: value, Stack: stack, At: time.Now(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "cn: crash: no report written: %v\n", err)
	} else {
		fmt.Fprintf(stderr, "cn: crash: report written to %s\n", path)
	}
	if isHook {
		return 0
	}
	return 1
}
