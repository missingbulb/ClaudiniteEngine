package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// hookIndex gives the hooks the rules index writer.
type hookIndex struct{}

// Write refreshes the generated files where the member holds them and
// moves nothing: a hook leaves no CLAUDE.md edit in the session's tree.
func (hookIndex) Write(repo, engine string) (bool, error) {
	written, err := rulesindex.Refresh(repo, engine)
	return len(written) > 0, err
}

func (hookIndex) HasImport(repo string) bool { return rulesindex.ImportsHeldIndex(repo) }

func (hookIndex) HasRules(repo, engine string) bool {
	st, _, err := rulesindex.Check(repo, engine)
	return err == nil && st != rulesindex.Empty
}

func cmdRulesIndex(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rules-index", flag.ContinueOnError)
	check := fs.Bool("check", false, "")
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *check {
		st, _, err := rulesindex.Check(*repo, version.Version())
		if err != nil {
			return report.New(report.IO, err.Error())
		}
		fmt.Fprintf(stdout, "%s %s\n", rulesindex.File, st)
		if st == rulesindex.Stale || st == rulesindex.Absent {
			return report.New(report.Verify, fmt.Sprintf("%s is %s; run cn rules-index", rulesindex.File, st))
		}
		return nil
	}
	written, err := rulesindex.Converge(*repo, version.Version())
	if err != nil {
		return report.New(report.IO, err.Error())
	}
	for _, f := range written {
		verb := "wrote"
		if _, err := os.Stat(filepath.Join(*repo, filepath.FromSlash(f))); errors.Is(err, os.ErrNotExist) {
			verb = "removed"
		}
		fmt.Fprintf(stdout, "%s %s\n", verb, f)
	}
	if len(written) == 0 {
		fmt.Fprintf(stdout, "%s already current\n", rulesindex.File)
	}
	return nil
}
