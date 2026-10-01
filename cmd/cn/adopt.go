package main

import (
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/adopt"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

func cmdInit(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	packList := fs.String("packs", "", "")
	channel := fs.String("channel", "stable", "")
	pkg := fs.String("package", adopt.DefaultPackage, "")
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *packList == "" {
		return report.New(report.Usage, "init needs --packs ID[,ID]")
	}
	roots, err := license.Roots()
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
	reader, closeReader := packReader(roots, stdout)
	defer closeReader()
	err = adopt.Init(adopt.Input{
		Repo: *repo, Packs: strings.Split(*packList, ","), Channel: *channel, Package: *pkg,
		Fetch:  update.FetchInput{Registry: reg, Roots: roots, CacheRoot: paths.CacheRoot(), Platform: version.Platform(), Now: time.Now()},
		Reader: reader, Timeout: childTimeout, Out: stdout, Key: initKey(stderr),
	})
	if err != nil {
		return report.Wrap(report.IO, "init", err)
	}
	return nil
}

func cmdAdopt(args []string, stdout io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return report.New(report.Usage, "adopt takes a pack id")
	}
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	roots, err := license.Roots()
	if err != nil {
		return report.Wrap(report.Internal, "adopt", err)
	}
	reader, closeReader := packReader(roots, stdout)
	defer closeReader()
	if err := adopt.Adopt(adopt.AdoptInput{Repo: *repo, ID: args[0], Reader: reader, Out: stdout}); err != nil {
		return report.Wrap(report.IO, "adopt", err)
	}
	return nil
}
