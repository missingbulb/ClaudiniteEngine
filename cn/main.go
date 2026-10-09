// Command cn is the Claudinite engine.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	_ "github.com/missingbulb/ClaudiniteEngine/cn/fleet/checks"
	_ "github.com/missingbulb/ClaudiniteEngine/cn/fleet/pack"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/hooks"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/selftest"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/trust"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

const usage = `usage: cn <command> [arguments]

For people and sessions:
  adopt [ID[,ID]] [--answer PACK/Q=TEXT]... [--channel stable|canary|staging]
        [--belongs TEXT] [--excludes TEXT] [--repo DIR]
                 the one adoption verb. On a repo with no settings file: pin
                 the newest allowed engine, write the member files, vendor
                 the packs and what they require. On a member: declare and
                 vendor more packs; local/<name> scaffolds and declares a
                 local pack (--belongs, --excludes). With no ID: record the
                 answers and rewrite the rules index, skills index and flat
                 task declarations. Ends on the QUESTIONS, HANDOVER and
                 NEXT blocks
  verify [--repo DIR]
                 check a member's files against this engine version;
                 exit 1 on a break
  check --tag TAG | --pack ID [--transcript PATH] [-v] [--repo DIR]
                 run a slice of the declared packs' checks; exit 1 on a
                 finding; --transcript is the session the work checks read
  check list [--tasks] [--repo DIR]
                 every check, or with --tasks every task, the active packs
                 and the engine contribute
  work create <pack>/<task> [--urgent] [--context T] [--not-before ISO]
                 [--blocked-by #N,#M] [--qualifier T] [--supersedes #N]
                 file a work item by hand; an unqualified item for a
                 scheduled task is refused
  growth capture (--pr N | --issue N) [--transcript PATH] [--session ID]
                 [--branch NAME] [--repo DIR]
                 push the session's transcript, scrubbed, as a delta onto
                 the conversation-logs branch; session-end runs it too
  provenance mark <pack>|--all [--dry-run]
  provenance check <pack>|--all
  provenance append <pack> <element> [--kind K] [--date D] [--changed]
                 [--backfill] < entry.md
  provenance history <pack> <element>
  provenance backfill <pack> [<element>...]
  provenance backfill <pack> --apply <brief.md> [--backfill]
  provenance reduce <file> [--public]
                 a pack's provenance: markers and empty files, the audit
                 (exit 1 on a fault), one entry appended, one element's
                 raw evidence, the backfill brief and its edited entries
                 applied, a file reduced for the canon; <pack> is an id, a
                 path or local/<name>
  workflows diff [--repo DIR] [--name OWNER/NAME]
                 the patch that brings a member's workflows to this
                 version's templates; empty when they match. The name
                 (else GITHUB_REPOSITORY, else the origin remote) gives a
                 scheduler cron the hash did not write the repo's own
  version        print the engine version

For machinery:
  hook <event>   answer a Claude Code hook: session-start, pre-tool-use,
                 post-tool-use, user-prompt-submit, stop, session-end
  check world --pr-author LOGIN --base-ref REF [--repo DIR]
                 the CI gate: the pin and launcher guard, verify, then the
                 declared packs' world-tagged checks
  check sdk --out DIR
                 write the Go check SDK and its go.mod stanza, for a pack
                 repo's own tests
  update engine [--force] [--repo DIR]
                 propose or land the newest allowed engine version as a
                 pin-only PR; needs GITHUB_TOKEN; ends on its verdict line
  update packs [--force] [--repo DIR]
                 propose or land the declared packs' newest allowed
                 versions as a pack-only PR, once this repo's check world
                 passes over it; needs GITHUB_TOKEN
  update land --pr N --sha SHA [--repo DIR]
                 merge update PR N (engine or packs), whose CI passed on SHA;
                 an engine PR changing .github/workflows/ is skipped for its
                 agent stage to merge
  update land --check --base SHA --head SHA [--repo DIR]
                 an engine update PR's landing gate from git alone, both
                 commits fetched: no GitHub call, no write; exit 1 with the
                 reason it fails
  schedule run [--dry-run] [--repo DIR]
                 the scheduler run: repair, ask every scheduled task,
                 ready, adopt, reclaim; publishes the drain gate; needs
                 GITHUB_TOKEN
  schedule report-failure
                 file, or comment on, the one scheduler failure issue
  execute loop [--repo DIR]
                 the executor: claim, re-evaluate, run and converge every
                 ready item; needs GITHUB_TOKEN
  execute dispatch [--continue]
                 dispatch the executor workflow on the default branch;
                 with --continue, the next run after one died, or past
                 the chain's depth its report
  work validate --issue N --nonce X --item-file PATH --comments-file PATH
                 [--request-file PATH] [--repo DIR]
                 a routine session's entry gate: the hand-off's nonce;
                 for a task that delivers a pull request, the repo's
                 delivery
  work converge --issue N --outcome O --summary T [--pr N] --repo R
                 --item-file PATH
                 print the transition a routine session performs to
                 converge its item; refuses an item it does not hold
  work converge --issue N --record-failed <pack>/<task>
                 print the failed execution record of a convergence that
                 was refused or could not run
  settings config <pack> [--repo DIR]
                 a declared pack entry's config as JSON (null where it
                 carries none), for a pack's own script; exit 1 when the
                 pack is not declared
  fleet roster|update|add-packs|pack-seeds|judge|token|protocol|
        create-dashboard-artifact|promote-scope
                 a fleet manager's commands; cn fleet with no verb lists
                 them
`

// secretScanPlant is set only by the secret scan's own test build, to prove
// the scan finds a planted string; it is empty in every real build.
var secretScanPlant string

// runHook is a variable so a test can make a hook panic.
var runHook = hooks.Handler{Checks: hookChecks{}, Guards: hookGuards{}, Index: hookIndex{}, Growth: hookGrowth{}, UserPack: hookUserPack{}}.Run

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
	if report.CodeOf(err) == report.Usage && !report.IsQuiet(err) {
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
	args = noteRetired(args, stderr)
	switch args[0] {
	case "hook":
		if len(args) != 2 {
			return report.New(report.Usage, "hook takes exactly one event")
		}
		return runHook(args[1], stdin, stdout, stderr, start)
	case "version":
		if len(args) != 1 {
			return report.New(report.Usage, "version takes no arguments")
		}
		version.Print(stdout)
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
		roots, err := trust.Roots()
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
	case "adopt":
		return cmdAdopt(args[1:], stdout)
	case "settings":
		return cmdSettings(args[1:], stdout)
	case "tasks":
		return cmdTasks(args[1:], stdout)
	case "schedule":
		return cmdSchedule(args[1:], stdout)
	case "work":
		return cmdWork(args[1:], stdout)
	case "execute":
		return cmdExecute(args[1:], stdout)
	case "growth":
		return cmdGrowth(args[1:], stdout, stderr, start)
	case "provenance":
		return cmdProvenance(args[1:], stdin, stdout, stderr)
	case "fleet":
		return cmdFleet(args[1:], stdout, stderr, start)
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
