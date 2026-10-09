package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/rules/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/scaffold"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

const packUsage = "pack takes new <name> [--belongs TEXT] [--excludes TEXT] [--repo DIR]"

// cmdPack is `cn pack new <name>`: the local pack a repo's own lessons
// land in, scaffolded and declared, and the rules index converged so the
// pack's rules reach the next session.
func cmdPack(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "new" {
		return report.New(report.Usage, packUsage)
	}
	fs := flag.NewFlagSet("pack new", flag.ContinueOnError)
	belongs := fs.String("belongs", "", "")
	excludes := fs.String("excludes", "", "")
	repo := fs.String("repo", ".", "")
	// The name comes first; flags follow it.
	var name string
	rest := args[1:]
	if len(rest) > 0 && len(rest[0]) > 0 && rest[0][0] != '-' {
		name, rest = rest[0], rest[1:]
	}
	if err := flags(fs, rest); err != nil {
		return err
	}
	if name == "" {
		return report.New(report.Usage, "pack new needs a name")
	}
	root, err := filepath.Abs(*repo)
	if err != nil {
		return report.Wrap(report.IO, "pack new", err)
	}
	made, err := scaffold.New(scaffold.Request{Repo: root, Name: name, Belongs: *belongs, Excludes: *excludes, Now: time.Now()})
	if err != nil {
		return report.Wrap(report.Verify, "pack new", err)
	}
	for _, f := range made.Files {
		fmt.Fprintln(stdout, f)
	}
	fmt.Fprintf(stdout, "declared %s in %s\n", made.Token, made.Settings)
	written, err := rulesindex.Converge(root, version.Version())
	if err != nil {
		return report.Wrap(report.IO, "pack new: the rules index", err)
	}
	for _, f := range written {
		fmt.Fprintf(stdout, "wrote %s\n", f)
	}
	return nil
}
