// Command cn is the Claudinite engine.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/hooks"
	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle"
	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
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
  selftest       check this machine can run the engine
  verify [--repo DIR]
                 check a member's files against this engine version;
                 exit 1 on a break
  check world --pr-author LOGIN --base-ref REF [--repo DIR]
                 the CI gate: the pin and launcher guard, then verify
  update engine [--force] [--repo DIR]
                 propose or land the newest allowed engine version as a
                 pin-only PR; needs GITHUB_TOKEN; ends on its verdict line
  update land --pr N --sha SHA [--repo DIR]
                 merge update PR N, whose CI passed on SHA
`

// secretScanPlant is set only by the secret scan's own test build, to prove
// the scan finds a planted string; it is empty in every real build.
var secretScanPlant string

// runHook is a variable so a test can make a hook panic.
var runHook = hooks.Run

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

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
		if len(args) != 1 {
			return report.New(report.Usage, "version takes no arguments but --day")
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
		if len(args) != 1 {
			return report.New(report.Usage, "selftest takes no arguments")
		}
		in := lifecycle.SelftestInput{CacheRoot: paths.CacheRoot(), Now: time.Now()}
		roots, err := license.Roots()
		in.RootsErr = err
		for _, r := range roots {
			in.RootIDs = append(in.RootIDs, sign.KeyID(r))
		}
		if lifecycle.Selftest(stdout, in) != 0 {
			return report.New(report.IO, "selftest failed")
		}
		return nil
	case "verify":
		return cmdVerify(args[1:], stdout)
	case "check":
		return cmdCheck(args[1:], stdout)
	case "update":
		return cmdUpdate(args[1:], stdout)
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
