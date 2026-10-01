package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// hookIndex gives the hooks the rules index writer.
type hookIndex struct{}

func (hookIndex) Write(repo, engine string) (bool, error) { return rulesindex.Write(repo, engine) }

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
	changed, err := rulesindex.Write(*repo, version.Version())
	if err != nil {
		return report.New(report.IO, err.Error())
	}
	if changed {
		fmt.Fprintf(stdout, "wrote %s\n", rulesindex.File)
	} else {
		fmt.Fprintf(stdout, "%s already current\n", rulesindex.File)
	}
	return nil
}
