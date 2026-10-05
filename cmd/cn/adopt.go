package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/adopt"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/trust"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

func cmdInit(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	packList := fs.String("packs", "", "")
	channel := fs.String("channel", "stable", "")
	pkg := fs.String("package", adopt.DefaultPackage, "")
	repo := fs.String("repo", ".", "")
	fromNode := fs.Bool("from-node", false, "")
	var answers answerFlags
	fs.Var(&answers, "answer", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *fromNode && *packList != "" {
		return report.New(report.Usage, "init --from-node reads the packs from the Node declaration; drop --packs")
	}
	if *packList == "" && !*fromNode {
		return report.New(report.Usage, "init needs --packs ID[,ID]; basics is the usual first pack (it requires claudinite-lifecycle and git-github), and the vendored branch's directory lists the rest")
	}
	roots, err := trust.Roots()
	if err != nil {
		return report.Wrap(report.Internal, "init", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return report.Wrap(report.Internal, "init", err)
	}
	if err := adopt.CheckOwnManifest(exe, roots, version.Platform(), time.Now()); err != nil {
		return report.Wrap(report.Verify, "init", err)
	}
	reg, err := npmreg.FromEnv()
	if err != nil {
		return report.Wrap(report.IO, "init", err)
	}
	reader, closeReader, err := packReader(*repo, roots, stdout)
	if err != nil {
		return report.Wrap(report.IO, "init", err)
	}
	defer closeReader()
	in := adopt.Input{
		Repo: *repo, FullName: initFullName(*repo), Channel: *channel, Package: *pkg,
		Fetch:  update.FetchInput{Registry: reg, Roots: roots, CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now()},
		Reader: reader, Timeout: childTimeout, Out: stdout, Answers: answers,
	}
	if *fromNode {
		err = adopt.FromNode(in)
	} else {
		in.Packs = strings.Split(*packList, ",")
		err = adopt.Init(in)
	}
	if err != nil {
		return report.Wrap(report.IO, "init", err)
	}
	return nil
}

func cmdAdopt(args []string, stdout io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return report.New(report.Usage, "adopt takes ID[,ID]")
	}
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	var answers answerFlags
	fs.Var(&answers, "answer", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	roots, err := trust.Roots()
	if err != nil {
		return report.Wrap(report.Internal, "adopt", err)
	}
	reader, closeReader, err := packReader(*repo, roots, stdout)
	if err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	defer closeReader()
	if err := adopt.Adopt(adopt.AdoptInput{Repo: *repo, IDs: strings.Split(args[0], ","), Answers: answers, Reader: reader, Out: stdout}); err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	return nil
}

// originOf is the checkout's configured origin URL, unrewritten.
func originOf(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return "", errors.New("the checkout has no origin remote")
	}
	return strings.TrimSpace(string(out)), nil
}

// initFullName is the repo's name off its origin, or its directory's.
func initFullName(dir string) string {
	origin, _ := originOf(dir)
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return adopt.FullNameOf(origin, abs)
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
