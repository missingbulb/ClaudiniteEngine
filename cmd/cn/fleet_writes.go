package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/addpacks"
	"github.com/missingbulb/ClaudiniteEngine/fleet/seeds"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
)

// minPlausiblePacks is the smallest catalog a scan runs against: a
// shelf that quietly shrank would report every member fitted for the
// wrong reason. CLAUDINITE_FLEET_MIN_PACKS lowers it for a fixture shelf.
const minPlausiblePacks = 5

// fleetProtocol is `cn fleet protocol`: the add-packs work-list
// constants the member half's copy is held to.
func fleetProtocol(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fleet protocol", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	p := addpacks.Protocol()
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	for _, k := range []string{"label", "mark", "memberTaskId", "requestedTitle", "suspectedTitle"} {
		fmt.Fprintf(stdout, "%s: %s\n", k, p[k])
	}
	return nil
}

// splitCodeWork separates cn's own --repo and --api from the code_work
// line's --name=value parameters, which go to the sweep as its argv.
func splitCodeWork(args []string) (repo, api string, argv []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if name != "repo" && name != "api" || !strings.HasPrefix(a, "-") {
			argv = append(argv, a)
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return "", "", nil, report.New(report.Usage, "--"+name+" needs a value")
			}
			i++
			value = args[i]
		}
		if name == "repo" {
			repo = value
		} else {
			api = value
		}
	}
	return repo, api, argv, nil
}

// fleetAddPacks is `cn fleet add-packs`, the fleet-add-missing-packs
// task's code-work: the scheduled scan from its command line, a force
// from its item's Context.
func fleetAddPacks(args []string, stdout, stderr io.Writer, start time.Time) error {
	repo, api, argv, err := splitCodeWork(args)
	if err != nil {
		return err
	}
	s, err := openSweep("fleet-add-missing-packs", "add-packs", fleet.SweepAddPacks,
		"The default GITHUB_TOKEN sees only this repo and cannot reach the fleet.", repo, api, stderr, start)
	if err != nil {
		return err
	}
	defer s.closer()
	p, err := addpacks.ParseParams(argv, fleet.ParseParamBag(os.Getenv(fleet.ContextEnv)))
	if err != nil {
		s.crumb("add-packs", "refused", 0, 0)
		return s.failed(err)
	}
	if p.Forced {
		repos := strings.Join(p.Repos, " ")
		if repos == "" {
			repos = addpacks.AllMembers
		}
		added := strings.Join(p.AddPacks, " ")
		if added == "" {
			added = "none"
		}
		fmt.Fprintf(stdout, "FORCED run - scan=%v, repos=%s, packs=%s\n", p.Scan, repos, added)
	} else {
		repos := addpacks.AllMembers
		if p.Repos != nil {
			repos = strings.Join(p.Repos, " ")
		}
		fmt.Fprintf(stdout, "scheduled run - scan=%v, repos=%s\n", p.Scan, repos)
	}
	cat, err := s.packs.VerifiedCatalog()
	if err != nil {
		s.crumb("add-packs", "error", 0, 0)
		return s.failed(fmt.Errorf("the shelf's catalog could not be read: %w", err))
	}
	corpus := addpacks.CatalogCorpus(cat.Catalog)
	ids, onStable, onCanary := addpacks.Count(cat.Catalog)
	fmt.Fprintf(stdout, "catalog: %d pack(s), %d on stable and %d on canary, serial %d from %s\n", ids, onStable, onCanary, cat.Catalog.Serial, cat.From)
	floor := minPlausiblePacks
	if n, err := strconv.Atoi(os.Getenv("CLAUDINITE_FLEET_MIN_PACKS")); err == nil && n > 0 {
		floor = n
	}
	if ids < floor {
		s.crumb("add-packs", "error", 0, 0)
		return s.failed(fmt.Errorf("only %d pack(s) in the shelf's catalog across both channels — refusing to sweep the fleet against a corpus that small, because every member would report as fitted for the wrong reason", ids))
	}
	if err := addpacks.Validate(p, s.cfg.Owner, s.cfg, corpus.Union); err != nil {
		s.crumb("add-packs", "refused", 0, 0)
		return s.failed(err)
	}
	repos, err := addpacks.Enumerate(s.gh, s.cfg.Owner)
	if err != nil {
		s.crumb("add-packs", "error", 0, 0)
		return s.failed(err)
	}
	o, err := addpacks.Run(s.gh, repos, s.home, s.cfg, p, corpus)
	for _, l := range o.Logs {
		fmt.Fprintln(stdout, l)
	}
	if len(o.Summaries) > 0 {
		emit(stdout, strings.Join(o.Summaries, "\n\n"))
	}
	if err != nil {
		outcome := "error"
		if addpacks.IsRefusal(err) {
			outcome = "refused"
		}
		s.crumb("add-packs", outcome, o.Fits, o.Members)
		return s.failed(err)
	}
	if err := o.Err(); err != nil {
		s.crumb("add-packs", "unknown", o.Fits, o.Members)
		return s.failed(err)
	}
	s.crumb("add-packs", "ok", o.Fits, o.Members)
	return nil
}

// fleetPackSeeds is `cn fleet pack-seeds`, the fleet-pack-seeds task's
// code-work: the one sweep that writes into members' trees.
func fleetPackSeeds(args []string, stdout, stderr io.Writer, start time.Time) error {
	fs := flag.NewFlagSet("fleet pack-seeds", flag.ContinueOnError)
	repo := fs.String("repo", "", "")
	api := fs.String("api", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	s, err := openSweep("fleet-pack-seeds sweep", "pack-seeds", fleet.SweepPackSeeds,
		"This sweep writes a declaration into each member, so a read-only grant is not enough.", *repo, *api, stderr, start)
	if err != nil {
		return err
	}
	defer s.closer()
	if len(s.cfg.PackSeeds) == 0 {
		emit(stdout, seeds.NoSeeds(s.cfg.Owner))
		s.crumb("pack-seeds", "ok", 0, 0)
		return nil
	}
	repos, err := fleet.Enumerate(s.gh, s.cfg.Owner)
	if err != nil {
		s.crumb("pack-seeds", "error", 0, 0)
		return s.failed(err)
	}
	r := seeds.Sweep(s.gh, repos, s.home, s.cfg)
	emit(stdout, r.Render())
	members := len(r.Written) + len(r.Already) + len(r.Waiting) + len(r.Node) + len(r.Unknown)
	if err := r.Err(); err != nil {
		s.crumb("pack-seeds", "unknown", len(r.Written), members)
		return s.failed(err)
	}
	s.crumb("pack-seeds", "ok", len(r.Written), members)
	return nil
}
