package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/rules/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/adopt"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/fetch"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/scaffold"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/trust"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/update"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// cmdAdopt is `cn adopt [ID[,ID]] [--answer P/Q=T]... [--channel C]
// [--belongs T] [--excludes T] [--repo DIR]`, the one adoption verb: a
// repo with no settings file is adopted whole, an adopted one declares and
// vendors more packs, a local/<name> id scaffolds the local pack it names,
// and with no id the answers are recorded and the generated files the
// settings imply are converged.
func cmdAdopt(args []string, stdout io.Writer) error {
	var idList string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		idList, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	channel := fs.String("channel", "", "")
	belongs := fs.String("belongs", "", "")
	excludes := fs.String("excludes", "", "")
	repo := fs.String("repo", ".", "")
	var answers answerFlags
	fs.Var(&answers, "answer", "")
	if err := report.ParseFlags(fs, args); err != nil {
		return err
	}
	var canon, local []string
	if idList != "" {
		for _, id := range strings.Split(idList, ",") {
			if name, ok := strings.CutPrefix(id, settings.LocalPrefix); ok {
				local = append(local, name)
			} else {
				canon = append(canon, id)
			}
		}
	}
	if (*belongs != "" || *excludes != "") && len(local) != 1 {
		return report.New(report.Usage, "adopt takes --belongs and --excludes with exactly one local/<name>")
	}
	if !adopted(*repo) {
		if len(local) > 0 {
			return report.New(report.Usage, "adopt the repo first; a local pack is added once it is a member")
		}
		if len(canon) == 0 {
			return report.New(report.Usage, "adopt needs ID[,ID] to adopt this repo; basics is the usual first pack (it requires claudinite-lifecycle and git-github), and the vendored branch's directory lists the rest")
		}
		if *channel == "" {
			*channel = "stable"
		}
		return adoptFirst(*repo, *channel, canon, answers, stdout)
	}
	if *channel != "" {
		return report.New(report.Usage, "adopt takes --channel on a first adoption only; this repo is adopted")
	}
	root, err := filepath.Abs(*repo)
	if err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	for _, name := range local {
		made, err := scaffold.New(scaffold.Request{Repo: root, Name: name, Belongs: *belongs, Excludes: *excludes, Now: time.Now()})
		if err != nil {
			return report.Wrap(report.Verify, "adopt", err)
		}
		for _, f := range made.Files {
			fmt.Fprintln(stdout, f)
		}
		fmt.Fprintf(stdout, "declared %s in %s\n", made.Token, made.Settings)
	}
	if len(canon) > 0 {
		return adoptMore(root, canon, answers, stdout)
	}
	for _, a := range answers {
		file, err := adopt.Answer(root, version.Version(), a.Address, a.Text)
		if err != nil {
			return report.Wrap(report.Verify, "adopt", err)
		}
		fmt.Fprintf(stdout, "answered %s in %s\n", a.Address, file)
	}
	return convergeIndex(root, stdout)
}

// adopted is a repo holding a settings file in any format.
func adopted(repo string) bool {
	for _, f := range settings.Formats {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(settings.RelPath(f)))); err == nil {
			return true
		}
	}
	return false
}

// adoptFirst adopts a repo that is not yet a member: pins the newest
// allowed engine, writes the member files and vendors the packs.
func adoptFirst(repo, channel string, ids []string, answers answerFlags, stdout io.Writer) error {
	roots, err := trust.Roots()
	if err != nil {
		return report.Wrap(report.Internal, "adopt", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return report.Wrap(report.Internal, "adopt", err)
	}
	if err := adopt.CheckOwnManifest(exe, roots, version.Platform(), time.Now()); err != nil {
		return report.Wrap(report.Verify, "adopt", err)
	}
	reg, err := npmreg.FromEnv()
	if err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	reader, closeReader, err := fetch.ForRepo(repo, roots, stdout)
	if err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	defer closeReader()
	in := adopt.Input{
		Repo: repo, FullName: initFullName(repo), Channel: channel,
		Fetch:  update.FetchInput{Registry: reg, Roots: roots, CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now()},
		Reader: reader, Timeout: childTimeout, Out: stdout, Answers: answers, Packs: ids,
	}
	if err := adopt.Init(in); err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	return nil
}

// adoptMore declares and vendors more packs on an adopted repo.
func adoptMore(repo string, ids []string, answers answerFlags, stdout io.Writer) error {
	roots, err := trust.Roots()
	if err != nil {
		return report.Wrap(report.Internal, "adopt", err)
	}
	reader, closeReader, err := fetch.ForRepo(repo, roots, stdout)
	if err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	defer closeReader()
	if err := adopt.Adopt(adopt.AdoptInput{Repo: repo, IDs: ids, Answers: answers, Reader: reader, Out: stdout}); err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	return nil
}

// convergeIndex writes the rules index, the skills index and the flat
// declarations the settings imply, naming each file written or removed.
func convergeIndex(repo string, stdout io.Writer) error {
	written, err := rulesindex.Converge(repo, version.Version())
	if err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	for _, f := range written {
		verb := "wrote"
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(f))); errors.Is(err, os.ErrNotExist) {
			verb = "removed"
		}
		fmt.Fprintf(stdout, "%s %s\n", verb, f)
	}
	if len(written) == 0 {
		fmt.Fprintf(stdout, "%s already current\n", rulesindex.File)
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
