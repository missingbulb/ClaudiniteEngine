package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/decide"
	"github.com/missingbulb/ClaudiniteEngine/fleet/entitlement"
	"github.com/missingbulb/ClaudiniteEngine/fleet/roster"
	"github.com/missingbulb/ClaudiniteEngine/fleet/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/trust"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
	"github.com/missingbulb/ClaudiniteEngine/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

func cmdFleet(args []string, stdout, stderr io.Writer, start time.Time) error {
	if len(args) == 0 {
		return report.New(report.Usage, "fleet takes roster, update, add-packs, pack-seeds, judge, token, protocol or decide")
	}
	switch args[0] {
	case "roster":
		return fleetRoster(args[1:], stdout, stderr, start)
	case "update":
		return fleetUpdate(args[1:], stdout, stderr, start)
	case "judge":
		return fleetJudge(args[1:], stdout, stderr, start)
	case "token":
		return fleetToken(args[1:], stdout)
	case "protocol":
		return fleetProtocol(args[1:], stdout)
	case "add-packs":
		return fleetAddPacks(args[1:], stdout, stderr, start)
	case "pack-seeds":
		return fleetPackSeeds(args[1:], stdout, stderr, start)
	case "decide":
		return fleetDecide(args[1:], stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown fleet command %q", args[0]))
}

// fleetToken is `cn fleet token`: the grant table, the pack's handover
// step held to it.
func fleetToken(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fleet token", flag.ContinueOnError)
	sweep := fs.String("sweep", "", "")
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *asJSON {
		out := map[string]any{"grant": fleet.Grant(), "permissions": fleet.Permissions, "handover": fleet.HandoverStep()}
		if *sweep != "" {
			out["sweep"] = *sweep
			out["uses"] = fleet.GrantFor(*sweep)
		}
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	fmt.Fprintf(stdout, "%s must be granted: %s\n", fleet.TokenEnv, fleet.Grant())
	for _, p := range fleet.Permissions {
		fmt.Fprintf(stdout, "- %s: %s — %s\n", p.Permission, p.Access, p.Why)
	}
	if *sweep != "" {
		fmt.Fprintf(stdout, "%s uses: %s\n", *sweep, fleet.GrantFor(*sweep))
	}
	return nil
}

func fleetDecide(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "fleet decide needs a core")
	}
	fs := flag.NewFlagSet("fleet decide", flag.ContinueOnError)
	world := fs.String("world", "", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	raw, err := os.ReadFile(*world)
	if err != nil {
		return report.Wrap(report.IO, "fleet decide", err)
	}
	answer, err := decide.Decide(args[0], raw)
	if err != nil {
		return report.New(report.Usage, err.Error())
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(answer)
}

// sweep is what every fleet sweep reads before it touches a member: the
// owner's entitlement, the token, the manager and its config, the API and
// the shelf.
type sweep struct {
	name     string
	entitled entitlement.Verdict
	gh       fleet.GH
	home     string
	root     string
	cfg      fleet.Config
	shelf    fleet.Shelf
	packs    *packs.Reader
	stderr   io.Writer
	start    time.Time
	closer   func()
}

// failed prints a failure; a grant error also prints the action marker,
// which the executor's park comment names as the worker's verdict.
func (s *sweep) failed(err error) error {
	fmt.Fprintf(s.stderr, "%s failed: %s\n", s.name, err.Error())
	if fleet.IsGrant(err) {
		fmt.Fprintf(s.stderr, "claudinite-needs-human: %s — %s\n", fleet.Triage, err.Error())
	}
	return report.Said(report.IO)
}

// crumb is the sweep's one breadcrumb, `[cn] fleet <event> <outcome>
// <n>/<m> <ms>ms`: a count of what the sweep reached, which the shared
// line has no field for. An unverified run's ok is `unverified`, so it can
// be counted.
func (s *sweep) crumb(event, outcome string, n, m int) {
	if outcome == "ok" && s.entitled.Unverified {
		outcome = "unverified"
	}
	fmt.Fprintf(s.stderr, "[cn] fleet %s %s %d/%d %dms\n", event, outcome, n, m, time.Since(s.start).Milliseconds())
}

// openSweep checks the entitlement, the token and the config, in that order,
// reading nothing of any member until all three pass.
func openSweep(name, event, sweepID, missingDetail, repo, api string, stderr io.Writer, start time.Time) (*sweep, error) {
	s := &sweep{name: name, stderr: stderr, start: start, closer: func() {}}
	root, err := fleetRoot(repo)
	if err != nil {
		s.crumb(event, "error", 0, 0)
		return nil, report.Wrap(report.IO, name, err)
	}
	s.root = root
	v, err := fleetCheck()
	if err != nil {
		s.crumb(event, "error", 0, 0)
		return nil, report.Wrap(report.Internal, name, err)
	}
	if v.Refused {
		fmt.Fprintln(stderr, v.Notice)
		fmt.Fprintf(stderr, "claudinite-needs-human: %s — %s\n", fleet.Triage, v.Notice)
		s.crumb(event, "refused", 0, 0)
		return nil, report.Said(report.IO)
	}
	if v.Transient {
		fmt.Fprintln(stderr, v.Notice)
		s.crumb(event, "error", 0, 0)
		return nil, report.Said(report.IO)
	}
	if v.Unverified {
		fmt.Fprintln(stderr, v.Notice)
		appendSummary(v.Notice)
	}
	s.entitled = v
	token := os.Getenv(fleet.TokenEnv)
	if token == "" {
		fmt.Fprintf(stderr, "%s failed: %s\n", name, fleet.MissingTokenError(sweepID, missingDetail))
		s.crumb(event, "error", 0, 0)
		return nil, report.Said(report.IO)
	}
	home, err := fleetHome(root)
	if err != nil {
		s.crumb(event, "error", 0, 0)
		return nil, report.Wrap(report.IO, name, err)
	}
	s.home = home
	cfg, err := fleetConfig(root, home)
	if err != nil {
		fmt.Fprintf(stderr, "%s failed: %s\n", name, err.Error())
		s.crumb(event, "error", 0, 0)
		return nil, report.Said(report.IO)
	}
	if cfg.CanonRepoNamed {
		fmt.Fprintln(stderr, fleet.CanonRepoNote)
	}
	s.cfg = cfg
	if api == "" {
		api = os.Getenv("CLAUDINITE_GITHUB_API")
	}
	if api == "" {
		api = githubapi.DefaultBase
	}
	s.gh = fleet.NewGH(api, token)
	shelf, reader, closer, err := fleetShelf(root, stderr)
	if err != nil {
		s.crumb(event, "error", 0, 0)
		return nil, report.Wrap(report.IO, name, err)
	}
	s.shelf, s.packs, s.closer = fleet.Memo(shelf), reader, closer
	return s, nil
}

// fleetRoot is the manager's checkout: --repo, else the executor's
// CLAUDINITE_REPO_ROOT (code-work runs in its task's folder), else the
// working directory.
func fleetRoot(repo string) (string, error) {
	if repo == "" {
		repo = os.Getenv("CLAUDINITE_REPO_ROOT")
	}
	if repo == "" {
		repo = "."
	}
	return filepath.Abs(repo)
}

// fleetHome is the manager's owner/name: GITHUB_REPOSITORY, else the
// executor's CLAUDINITE_REPO, else the checkout's origin remote.
func fleetHome(root string) (string, error) {
	for _, k := range []string{"GITHUB_REPOSITORY", "CLAUDINITE_REPO"} {
		if r := os.Getenv(k); strings.Contains(r, "/") {
			return r, nil
		}
	}
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	if err == nil {
		if r, ok := githubapi.ParseRemote(strings.TrimSpace(string(out))); ok {
			return r, nil
		}
	}
	return "", errors.New("GITHUB_REPOSITORY is not set and the checkout's origin names no GitHub repository")
}

// fleetConfig reads the manager's own claudinite-fleet-sheepdog entry out
// of its checkout.
func fleetConfig(root, home string) (fleet.Config, error) {
	path, f, err := settings.Find(root)
	if err != nil {
		return fleet.Config{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fleet.Config{}, err
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return fleet.Config{}, err
	}
	for _, e := range p.Entries {
		if !e.Local && e.ID == fleet.PackID {
			if e.Config == nil {
				break
			}
			return fleet.ParseConfig(e.Config, true, home)
		}
	}
	return fleet.ParseConfig(nil, false, home)
}

// shelfReader is the shelf a member's own update reads: npm and the
// pack indexes.
type shelfReader struct {
	npm   npmreg.Client
	packs *packs.Reader
}

func (s shelfReader) Packument(pkg string) (*npmreg.Packument, error) { return s.npm.Packument(pkg) }

func (s shelfReader) Index(id string) (packindex.Index, bool, error) {
	v, err := s.packs.VerifiedIndex(id)
	if err != nil {
		return packindex.Index{}, false, err
	}
	return v.Index, true, nil
}

func fleetShelf(root string, log io.Writer) (fleet.Shelf, *packs.Reader, func(), error) {
	reg, err := npmreg.FromEnv()
	if err != nil {
		return nil, nil, nil, err
	}
	roots, err := trust.Roots()
	if err != nil {
		return nil, nil, nil, err
	}
	r, closer, err := packReader(root, roots, log)
	if err != nil {
		return nil, nil, nil, err
	}
	return shelfReader{npm: reg, packs: r}, r, closer, nil
}

// fleetCheck is the run's one license check; a test replaces it.
var fleetCheck = fleetEntitlement

// fleetEntitlement is the license check over the job's Actions key.
func fleetEntitlement() (entitlement.Verdict, error) {
	roots, err := trust.Roots()
	if err != nil {
		return entitlement.Verdict{}, err
	}
	in := entitlement.In{Getenv: os.Getenv, Roots: roots, Now: time.Now(), Engine: version.Version(),
		OIDC: func() (string, error) {
			return githubapi.OIDCToken(&http.Client{Timeout: 10 * time.Second}, os.Getenv, entitlement.Audience)
		}}
	c, err := licenseClient()
	if err != nil {
		return entitlement.Verdict{}, err
	}
	in.Server = c
	return entitlement.Check(in), nil
}

// licenseClient builds the license server's client; a test replaces it.
var licenseClient = licenseapi.FromEnv

// reach is repos cut to the ones the run's entitlement covers, each other
// one named on stderr; nil with an error when it covers none of them.
func (s *sweep) reach(repos []fleet.Repo) ([]fleet.Repo, error) {
	var out []fleet.Repo
	for _, r := range repos {
		if s.entitled.Allows(r.FullName, r.Owner.ID) {
			out = append(out, r)
			continue
		}
		fmt.Fprintln(s.stderr, s.entitled.RepoNotice(r.FullName))
	}
	if len(out) == 0 && len(repos) > 0 {
		return nil, &fleet.GrantError{Msg: fmt.Sprintf("the fleet's plan covers none of its %d repo(s): they are owned by an account other than %s", len(repos), s.entitled.OwnerLogin)}
	}
	return out, nil
}

// emit prints the report and appends it to the step summary.
func emit(stdout io.Writer, summary string) {
	fmt.Fprintln(stdout, summary)
	appendSummary(summary)
}

// appendSummary appends text to the step summary, when the job has one.
func appendSummary(text string) {
	if p := os.Getenv("GITHUB_STEP_SUMMARY"); p != "" {
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, text)
			_ = f.Close()
		}
	}
}

// fleetRoster is `cn fleet roster`, the fleet-roster task's code-work.
func fleetRoster(args []string, stdout, stderr io.Writer, start time.Time) error {
	fs := flag.NewFlagSet("fleet roster", flag.ContinueOnError)
	repo := fs.String("repo", "", "")
	api := fs.String("api", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	s, err := openSweep("fleet-roster sweep", "roster", fleet.SweepRoster,
		"The default GITHUB_TOKEN sees only this repo and cannot sweep the fleet.", *repo, *api, stderr, start)
	if err != nil {
		return err
	}
	defer s.closer()
	repos, err := fleet.Enumerate(s.gh, s.cfg.Owner)
	if err != nil {
		if errors.Is(err, fleet.ErrNoOwnedRepos) {
			err = fmt.Errorf("%w; refusing to run a sweep that would close every adoption issue as stale", err)
		}
		s.crumb("roster", "error", 0, 0)
		return s.failed(err)
	}
	if repos, err = s.reach(repos); err != nil {
		s.crumb("roster", "refused", 0, 0)
		return s.failed(err)
	}
	r := roster.Build(s.gh, repos, s.home, s.cfg, s.shelf)
	cov, fresh := roster.CoverageView(r), roster.FreshnessView(r)
	covered, total := len(cov.Covered)+len(cov.Dormant), len(r)
	if err := fleet.EnsureLabel(s.gh, s.home, roster.Label, roster.LabelColor, roster.LabelDescription); err != nil {
		s.crumb("roster", "error", covered, total)
		return s.failed(err)
	}
	actions, err := roster.ConvergeAdoption(s.gh, s.home, cov.Uncovered, append(append([]string{}, cov.Covered...), cov.Dormant...), cov.Ignored)
	if err != nil {
		s.crumb("roster", "error", covered, total)
		return s.failed(err)
	}
	emit(stdout, roster.RenderCoverage(s.cfg.Owner, s.home, cov, actions)+"\n\n"+roster.RenderFreshness(s.cfg.Owner, s.home, fresh))
	clock, err := world.Env(os.Getenv).Clock()
	if err != nil {
		s.crumb("roster", "error", covered, total)
		return s.failed(err)
	}
	prior, from := landedRoster(s.root)
	fmt.Fprintf(stderr, "[cn] fleet roster: compared against %s\n", from)
	wrote, err := roster.WriteArtifact(s.root, s.cfg.Owner, roster.Verdicts(r), clock.Now().UTC().Format(time.RFC3339), prior)
	if err != nil {
		s.crumb("roster", "error", covered, total)
		return s.failed(err)
	}
	if wrote {
		fmt.Fprintf(stderr, "[cn] fleet roster: wrote %s\n", roster.RosterFile)
	} else {
		fmt.Fprintf(stderr, "[cn] fleet roster: %s unchanged\n", roster.RosterFile)
	}
	if unknown := roster.Unknowns(cov, fresh); len(unknown) > 0 {
		s.crumb("roster", "unknown", covered, total)
		msg := roster.UnknownError(len(unknown))
		for _, e := range r {
			if e.Grant {
				return s.failed(&fleet.GrantError{Msg: msg + ": " + strings.Join(unknown, "; ")})
			}
		}
		return s.failed(errors.New(msg))
	}
	s.crumb("roster", "ok", covered, total)
	return nil
}

// landedRoster is the roster file as the branch this run lands on holds
// it, and where it was read: the executor's target branch, else the
// default branch, each fetched over the job's token, else nil for the
// checkout's own copy. A recompute is judged against what already landed,
// not against a checkout the executor resets between items.
func landedRoster(root string) ([]byte, string) {
	git := gitcmd.Repo{Dir: root, Token: os.Getenv("GITHUB_TOKEN")}
	for _, ref := range []string{os.Getenv("CLAUDINITE_TARGET_BRANCH"), os.Getenv("CLAUDINITE_DEFAULT_BRANCH")} {
		if ref == "" {
			continue
		}
		if ran, err := git.Run("fetch", "--quiet", "origin", ref); err != nil || ran.Code != 0 {
			continue
		}
		ran, err := git.Run("show", "FETCH_HEAD:"+roster.RosterFile)
		if err == nil && ran.Code == 0 {
			return []byte(ran.Stdout), "origin/" + ref
		}
		return nil, "the checkout (origin/" + ref + " has no roster)"
	}
	return nil, "the checkout"
}

// fleetUpdate is `cn fleet update`, the fleet-update task's code-work;
// its parameters ride the item's Context.
func fleetUpdate(args []string, stdout, stderr io.Writer, start time.Time) error {
	fs := flag.NewFlagSet("fleet update", flag.ContinueOnError)
	repo := fs.String("repo", "", "")
	api := fs.String("api", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	s, err := openSweep("fleet-update", "update", fleet.SweepUpdate,
		"The default GITHUB_TOKEN sees only this repo and cannot dispatch another repo's workflow.", *repo, *api, stderr, start)
	if err != nil {
		return err
	}
	defer s.closer()
	bag := fleet.ParseParamBag(os.Getenv(fleet.ContextEnv))
	o := update.Options{DryRun: strings.EqualFold(bag["DRY_RUN"], "true"), IncludeDormant: strings.EqualFold(bag["INCLUDE_DORMANT"], "true"),
		Filter: update.ParseRepoFilter(bag["REPOS"], s.cfg.Owner), Now: func() string { return time.Now().UTC().Format(time.RFC3339) }}
	minutes, err := strconv.ParseFloat(bag["FOLLOW_MINUTES"], 64)
	if err != nil || minutes <= 0 {
		minutes = update.DefaultFollowMinutes
	}
	repos, err := fleet.Enumerate(s.gh, s.cfg.Owner)
	if err != nil {
		s.crumb("update", "error", 0, 0)
		return s.failed(err)
	}
	if repos, err = s.reach(repos); err != nil {
		s.crumb("update", "refused", 0, 0)
		return s.failed(err)
	}
	fired, skipped, failed := update.Force(s.gh, repos, s.cfg, s.shelf, o)
	var followed []update.Followed
	if !o.DryRun && len(fired) > 0 {
		poll := update.PollMS
		if ms, err := strconv.ParseInt(os.Getenv("CLAUDINITE_FLEET_POLL_MS"), 10, 64); err == nil && ms > 0 {
			update.PollMS = []int64{ms}
			defer func() { update.PollMS = poll }()
		}
		epoch := time.Now()
		followed = update.Follow(fired, update.Read(s.gh, s.shelf),
			func(f update.Fired) (bool, error) { return update.StartedSince(s.gh, f.Repo.FullName, f.FiredAt) },
			int64(minutes*60000), update.Clock{
				Now:   func() int64 { return time.Since(epoch).Milliseconds() },
				Sleep: func(ms int64) { time.Sleep(time.Duration(ms) * time.Millisecond) },
				Log:   func(l string) { fmt.Fprintln(stdout, l) },
			})
	}
	rep := update.Report{Owner: s.cfg.Owner, DryRun: o.DryRun, Filter: o.Filter, Fired: fired, Followed: followed, Skipped: skipped, Failed: failed}
	emit(stdout, rep.Render())
	current := 0
	for _, f := range followed {
		if update.IsSuccess(f.Outcome) {
			current++
		}
	}
	if v := rep.Verdict(); v != "" {
		outcome := "not-current"
		if len(failed) > 0 {
			outcome = "error"
		}
		s.crumb("update", outcome, current, len(fired))
		if rep.Grant() {
			return s.failed(&fleet.GrantError{Msg: v})
		}
		return s.failed(errors.New(v))
	}
	s.crumb("update", "ok", current, len(fired))
	return nil
}

// fleetJudge is `cn fleet judge <owner/name>`: the per-repo half for one
// repository, for a person diagnosing a member without a sweep.
func fleetJudge(args []string, stdout, stderr io.Writer, start time.Time) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") || !strings.Contains(args[0], "/") {
		return report.New(report.Usage, "fleet judge needs an owner/name")
	}
	target := args[0]
	fs := flag.NewFlagSet("fleet judge", flag.ContinueOnError)
	repo := fs.String("repo", "", "")
	api := fs.String("api", "", "")
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	s, err := openSweep("fleet judge", "judge", fleet.SweepRoster, "", *repo, *api, stderr, start)
	if err != nil {
		return err
	}
	defer s.closer()
	if !s.cfg.Owns(strings.ToLower(target)) {
		fmt.Fprintf(stderr, "fleet judge refused: %s is not under the fleet's owner %s — the fleet reaches no other\n", strings.ToLower(target), s.cfg.Owner)
		s.crumb("judge", "refused", 0, 1)
		return report.Said(report.IO)
	}
	r, err := fleet.ReadRepo(s.gh, target)
	if err != nil {
		s.crumb("judge", "error", 0, 1)
		return s.failed(err)
	}
	if _, err := s.reach([]fleet.Repo{r}); err != nil {
		s.crumb("judge", "refused", 0, 1)
		return report.Said(report.IO)
	}
	v := fleet.JudgeRepo(s.gh, r, s.home, s.cfg, s.shelf)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		_ = enc.Encode(v)
	} else {
		fmt.Fprintf(stdout, "%s: scope %s", v.Repo, v.Scope)
		if v.Shape != "" {
			fmt.Fprintf(stdout, ", shape %s", v.Shape)
		}
		if v.Settings != "" {
			fmt.Fprintf(stdout, " (%s)", v.Settings)
		}
		if v.Dormant {
			fmt.Fprint(stdout, ", dormant")
		}
		fmt.Fprintln(stdout)
		if v.Freshness != nil {
			fmt.Fprintf(stdout, "freshness: %s — %s\n", v.Freshness.State, v.Freshness.Detail)
		}
		if v.Error != "" {
			fmt.Fprintf(stdout, "unknown: %s\n", v.Error)
		}
	}
	if v.Error != "" {
		s.crumb("judge", "unknown", 0, 1)
		return report.Said(report.IO)
	}
	s.crumb("judge", "ok", 1, 1)
	return nil
}

// fleetSignal is the collector's fleet reader when FLEET_GITHUB_TOKEN is
// in the env or the executor's secrets bag, over the run's own repo
// owner; nil without it.
func fleetSignal(repo string) func(sinceISO string) (any, error) {
	token, _ := execute.Secret(fleet.TokenEnv, map[string]string{
		fleet.TokenEnv: os.Getenv(fleet.TokenEnv), execute.SecretsBagEnv: os.Getenv(execute.SecretsBagEnv)})
	owner, _, ok := strings.Cut(repo, "/")
	if token == "" || !ok {
		return nil
	}
	api := os.Getenv("CLAUDINITE_GITHUB_API")
	if api == "" {
		api = githubapi.DefaultBase
	}
	gh := fleet.NewGH(api, token)
	return func(since string) (any, error) {
		s := fleet.ReadFleet(gh, owner, since)
		if s.Error != "" {
			return nil, errors.New(s.Error)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		var v any
		return v, json.Unmarshal(raw, &v)
	}
}
