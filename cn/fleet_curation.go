package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/curation"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/report"
)

const packHistoryUsage = "fleet pack-history takes [<id>...] [--ref REF] [--json] [--repo DIR]"

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

// fleetPackHistory is `cn fleet pack-history [<id>...] [--ref REF]
// [--json]`: each pack's last version move at REF (HEAD by default), the
// shipping files changed since, and the pull requests each version
// carried; the JSON is what the pack-version-history task renders its
// rows from.
func fleetPackHistory(args []string, stdout, _ io.Writer, _ time.Time) error {
	ref, repo, asJSON := "HEAD", "", false
	var ids []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--json":
			asJSON = true
		case "--ref", "--repo":
			if i+1 >= len(args) {
				return report.New(report.Usage, "fleet pack-history: "+a+" needs a value")
			}
			if a == "--ref" {
				ref = args[i+1]
			} else {
				repo = args[i+1]
			}
			i++
		default:
			if strings.HasPrefix(a, "-") {
				return report.New(report.Usage, packHistoryUsage)
			}
			ids = append(ids, a)
		}
	}
	root, err := fleetRoot(repo)
	if err != nil {
		return report.Wrap(report.IO, "fleet pack-history", err)
	}
	packs, err := curation.Walker{Git: gitcmd.Repo{Dir: root}}.History(ref, ids)
	if err != nil {
		return report.Wrap(report.Verify, "fleet pack-history", err)
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(packs)
	}
	for _, l := range curation.Lines(packs) {
		fmt.Fprintln(stdout, l)
	}
	return nil
}
