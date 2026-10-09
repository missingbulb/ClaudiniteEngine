package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/curation"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
)

// fleetPromoteScope is `cn fleet promote-scope --base REF`, the gate on a
// promote pull request: exit 0 where every changed path is under the
// corpus roots, 1 naming each stray path, 2 where the branch has no merge
// base with REF.
func fleetPromoteScope(args []string, stdout, stderr io.Writer, _ time.Time) error {
	fs := flag.NewFlagSet("fleet promote-scope", flag.ContinueOnError)
	base := fs.String("base", "", "")
	repo := fs.String("repo", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *base == "" {
		return report.New(report.Usage, "fleet promote-scope needs --base REF")
	}
	root, err := fleetRoot(*repo)
	if err != nil {
		return report.Wrap(report.IO, "fleet promote-scope", err)
	}
	res, err := curation.Check(root, *base)
	if errors.Is(err, curation.ErrNoMergeBase) {
		fmt.Fprintln(stderr, "promote-scope: "+err.Error()+".")
		return report.Said(report.Usage)
	}
	if err != nil {
		return report.Wrap(report.IO, "fleet promote-scope", err)
	}
	if len(res.Stray) > 0 {
		fmt.Fprintf(stderr, "promote-scope: FAIL — the promote phase may write only under %s, but this branch also touches %d path(s):\n", strings.Join(res.Roots, ", "), len(res.Stray))
		for _, p := range res.Stray {
			fmt.Fprintln(stderr, "  - "+p)
		}
		fmt.Fprintln(stderr, "\nHome each promoted lesson in the corpus; leave anything that can only live elsewhere local. Do not reach past the corpus roots.")
		return report.Said(report.Verify)
	}
	fmt.Fprintf(stdout, "promote-scope: OK — every changed path is under %s.\n", strings.Join(res.Roots, ", "))
	return nil
}
