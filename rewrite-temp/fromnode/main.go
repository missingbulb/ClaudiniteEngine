// Command fromnode moves a member repo off the Node engine onto cn. It
// is run by hand, with the owner, on a checkout of the member; nothing of
// it ships in cn.
//
//	fromnode [move] --repo DIR [--channel stable|canary|staging] [--answer PACK/Q=TEXT]...
//	    pin the newest allowed engine, import .claudinite-settings.json,
//	    vendor its canon packs again, replace the Node hooks and write the
//	    workflows; deletes no member file; ends on cn's QUESTIONS,
//	    HANDOVER and NEXT blocks, then LEFTOVERS
//	fromnode import [--from FILE] [--stdout] [--repo DIR]
//	    read the Node declaration into the pinned .claudinite/settings.*
//	    as its packs and checks blocks, one report line per key; --stdout
//	    prints the blocks as YAML instead
//	fromnode leftovers [--repo DIR]
//	    what the repo still carries of the Node engine
//
// Exit 0 when it did what was asked, 1 when it refused or a leftover is
// a break, 2 on a usage error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/adopt"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/trust"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// errUsage marks a usage error, exit 2.
var errUsage = errors.New("usage: fromnode [move|import|leftovers] [flags]; see the package comment")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	verb := "move"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb, args = args[0], args[1:]
	}
	var err error
	switch verb {
	case "move":
		err = cmdMove(args, stdout)
	case "import":
		err = cmdImport(args, stdout)
	case "leftovers":
		err = cmdLeftovers(args, stdout)
	default:
		err = errUsage
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintln(stderr, err)
		return 2
	default:
		fmt.Fprintln(stderr, "fromnode "+verb+": "+err.Error())
		return 1
	}
}

func parse(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected %q", errUsage, fs.Arg(0))
	}
	return nil
}

// answerFlags is a repeatable --answer <pack>/<question>=<text>.
type answerFlags []adopt.AnswerFlag

func (a *answerFlags) String() string { return "" }

func (a *answerFlags) Set(v string) error {
	f, err := adopt.ParseAnswerFlag(v)
	if err != nil {
		return err
	}
	*a = append(*a, f)
	return nil
}

func cmdMove(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("move", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	channel := fs.String("channel", "stable", "")
	var answers answerFlags
	fs.Var(&answers, "answer", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	roots, err := trust.Roots()
	if err != nil {
		return err
	}
	reg, err := npmreg.FromEnv()
	if err != nil {
		return err
	}
	srcs, closeSources := packs.SourcesFor(nil, &http.Client{Timeout: time.Minute})
	defer closeSources()
	in := adopt.Input{
		Repo: *repo, FullName: fullName(*repo), Channel: *channel,
		Fetch:  update.FetchInput{Registry: reg, Roots: roots, CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now()},
		Reader: &packs.Reader{Sources: srcs, Roots: roots, Now: time.Now, Log: stdout}, Timeout: 2 * time.Minute, Out: stdout, Answers: answers,
	}
	if err := move(in); err != nil {
		return err
	}
	return printLeftovers(*repo, stdout)
}

func cmdLeftovers(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("leftovers", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	return printLeftovers(*repo, stdout)
}

// printLeftovers prints the LEFTOVERS block, an error when one is a
// break.
func printLeftovers(repo string, stdout io.Writer) error {
	fs := leftovers(repo)
	fmt.Fprintf(stdout, "\nLEFTOVERS: %d\n", len(fs))
	for _, f := range fs {
		fmt.Fprintln(stdout, f.String())
	}
	if findings.AnyBreak(fs) {
		return errors.New("a leftover is a break")
	}
	return nil
}

// fullName is the repo's "owner/name" off its origin, or its directory's.
func fullName(dir string) string {
	origin, _ := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url").Output()
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return adopt.FullNameOf(string(origin), abs)
}
