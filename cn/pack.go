package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/growth/packhistory"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/scaffold"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

const packUsage = "pack takes new <name> [--belongs TEXT] [--excludes TEXT] [--repo DIR], or history [<id>...] [--ref REF] [--json] [--repo DIR]"

// cmdPack is `cn pack new <name>`: the local pack a repo's own lessons
// land in, scaffolded and declared, and the rules index converged so the
// pack's rules reach the next session; and `cn pack history`, a canon
// shelf's version walk.
func cmdPack(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "history" {
		return cmdPackHistory(args[1:], stdout)
	}
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

// cmdPackHistory is `cn pack history [<id>...] [--ref REF] [--json]`:
// each pack's last version move at REF (HEAD by default), the shipping
// files changed since, and the pull requests each version carried.
func cmdPackHistory(args []string, stdout io.Writer) error {
	ref, repo, asJSON := "HEAD", ".", false
	var ids []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--json":
			asJSON = true
		case "--ref", "--repo":
			if i+1 >= len(args) {
				return report.New(report.Usage, "pack history: "+a+" needs a value")
			}
			if a == "--ref" {
				ref = args[i+1]
			} else {
				repo = args[i+1]
			}
			i++
		default:
			if strings.HasPrefix(a, "-") {
				return report.New(report.Usage, packUsage)
			}
			ids = append(ids, a)
		}
	}
	root, err := filepath.Abs(repo)
	if err != nil {
		return report.Wrap(report.IO, "pack history", err)
	}
	packs, err := packhistory.Walker{Git: gitcmd.Repo{Dir: root}}.History(ref, ids)
	if err != nil {
		return report.Wrap(report.Verify, "pack history", err)
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(packs)
	}
	for _, l := range packhistory.Lines(packs) {
		fmt.Fprintln(stdout, l)
	}
	return nil
}
