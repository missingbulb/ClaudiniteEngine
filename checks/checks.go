package checks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/checks/run"
	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Service runs the declared packs' coded checks for one engine.
type Service struct {
	Build build.Config
	// Exe is this cn, which Start runs detached as `cn check build`.
	Exe string
}

// Key is the repo's checks binary key and its sources; "" when no declared
// pack has Go checks, or the repo has no settings file at all.
func (s Service) Key(repo string) (string, []build.Source, error) {
	if !hasSettings(repo) {
		return "", nil, nil
	}
	p, err := packset.Declared(repo)
	if err != nil {
		return "", nil, err
	}
	srcs, err := build.Sources(repo, p.Declared)
	if err != nil {
		return "", nil, err
	}
	return build.Key(s.Build, srcs), srcs, nil
}

// Start begins building the repo's checks binary in the background unless
// it is built or nothing needs building, and returns at once.
func (s Service) Start(repo string) error {
	key, _, err := s.Key(repo)
	if err != nil || key == "" {
		return err
	}
	if _, err := build.Wait(s.Build, key, 0); err == nil {
		return nil
	}
	return build.Start(s.Exe, repo, key)
}

// BuildNow builds the repo's checks binary in this process; when another
// process is building it, wait says whether to wait for that build, for
// up to timeout. It returns the key, "" for nothing to build.
func (s Service) BuildNow(repo, wantKey string, wait bool, timeout time.Duration) (string, error) {
	key, srcs, err := s.Key(repo)
	if err != nil {
		return "", err
	}
	if wantKey != "" && wantKey != key {
		return "", fmt.Errorf("the packs changed since key %s was computed (now %s)", wantKey, key)
	}
	err = build.Build(s.Build, key, srcs)
	if errors.Is(err, build.ErrBuilding) && wait {
		_, err = build.Wait(s.Build, key, timeout)
	}
	return key, err
}

// Run runs the checks whose tags include every one of tags (from pack when
// set) over repo. Foreground builds the binary here, as CI does; otherwise
// a build is started if none is under way and Run waits for it up to wait.
// The breadcrumb records the outcome: a binary not ready in time is a
// timeout, never silence. A repo with no Go checks runs nothing, ok.
func (s Service) Run(repo, event string, tags []string, pack string, wait time.Duration, foreground bool) (run.Result, string) {
	start := time.Now()
	key, srcs, err := s.Key(repo)
	if err == nil && key == "" {
		return run.Result{}, breadcrumb.Line("checks", event, breadcrumb.OK, time.Since(start))
	}
	var binary string
	if err == nil && foreground {
		err = build.Build(s.Build, key, srcs)
		if errors.Is(err, build.ErrBuilding) {
			err = nil
		}
	} else if err == nil {
		if _, werr := build.Wait(s.Build, key, 0); werr != nil {
			err = build.Start(s.Exe, repo, key)
		}
	}
	if err == nil {
		binary, err = build.Wait(s.Build, key, wait)
	}
	if err != nil {
		outcome := breadcrumb.Error
		if errors.Is(err, build.ErrTimeout) {
			outcome = breadcrumb.Timeout
		}
		return run.Result{Err: err}, breadcrumb.Line("checks", event, outcome, time.Since(start))
	}
	res, _ := run.Runner{Binary: binary, Engine: s.Build.Engine}.Run(event, tags, pack, repo)
	outcome := breadcrumb.OK
	switch {
	case res.Err != nil && errors.Is(res.Err, run.ErrSilent):
		outcome = breadcrumb.Timeout
	case res.Err != nil || len(res.Errors) > 0:
		outcome = breadcrumb.Error
	}
	return res, breadcrumb.Line("checks", event, outcome, time.Since(start))
}

// List returns the repo's coded checks, building the binary here.
func (s Service) List(repo string, timeout time.Duration) ([]run.Listed, error) {
	key, err := s.BuildNow(repo, "", true, timeout)
	if err != nil || key == "" {
		return nil, err
	}
	return run.Runner{Binary: s.Build.Binary(key), Engine: s.Build.Engine}.List()
}

func hasSettings(repo string) bool {
	for _, f := range settings.Formats {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(settings.RelPath(f)))); err == nil {
			return true
		}
	}
	return false
}

// Outcome is one run of both kinds of checks: the declared and built-in
// checks interpreted here, and the coded ones in the checks binary.
type Outcome struct {
	// Findings are both kinds' findings with the member's overrides and
	// acceptances applied, blocking first.
	Findings []findings.Finding
	// Errors are coded checks that failed inside a completed run.
	Errors []string
	// Err is a coded run that could not happen.
	Err error
	// Crumb is the checks breadcrumb; DeclaredCrumb the declared run's.
	Crumb, DeclaredCrumb string
}

// Blocking reports whether any finding blocks.
func (o Outcome) Blocking() bool { return findings.AnyBreak(o.Findings) }

// RunAll runs the declared checks sel takes in this process, then the
// coded ones with the same tags and pack as Run does, and applies the
// member's checks configuration over both.
func (s Service) RunAll(repo, event string, sel declared.Selection, wait time.Duration, foreground bool, stderr io.Writer) Outcome {
	tags, pack := sel.Tags, sel.Pack
	start := time.Now()
	var out Outcome
	set, err := declared.LoadSet(repo, s.Build.Engine)
	var cfg declared.Config
	n := 0
	if err != nil {
		out.Findings = append(out.Findings, findings.Finding{Class: findings.Break, ID: "checks-run", Path: ".claudinite", Sentence: "the declared checks could not load: " + err.Error()})
	} else {
		var fs []findings.Finding
		fs, n = set.Run(sel, time.Now(), stderr)
		out.Findings = append(out.Findings, fs...)
		cfg = set.Config
	}
	out.DeclaredCrumb = fmt.Sprintf("[cn] declared %d checks %dms", n, time.Since(start).Milliseconds())
	res, crumb := s.Run(repo, event, tags, pack, wait, foreground)
	out.Crumb, out.Errors, out.Err = crumb, res.Errors, res.Err
	out.Findings = append(out.Findings, res.Shared()...)
	out.Findings = declared.ApplyConfig(out.Findings, cfg)
	return out
}

// ListAll names the declared, built-in and coded checks, sorted by id.
func (s Service) ListAll(repo string, timeout time.Duration) ([]Listed, error) {
	var out []Listed
	set, err := declared.LoadSet(repo, s.Build.Engine)
	if err != nil {
		return nil, err
	}
	for _, l := range set.List() {
		out = append(out, Listed{ID: l.ID, Pack: l.Pack, Kind: l.Kind, Tags: l.Tags, OnFail: l.OnFail})
	}
	coded, err := s.List(repo, timeout)
	if err != nil {
		return nil, err
	}
	for _, c := range coded {
		pack, id, ok := strings.Cut(c.Check, "/")
		if !ok {
			pack, id = "", c.Check
		}
		out = append(out, Listed{ID: id, Pack: pack, Kind: "coded", Tags: c.Tags, OnFail: "block"})
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].ID != out[k].ID {
			return out[i].ID < out[k].ID
		}
		return out[i].Pack < out[k].Pack
	})
	return out, nil
}

// Listed is one check as cn check list prints it.
type Listed struct {
	ID, Pack, Kind string
	Tags           []string
	OnFail         string
}

// Name is the check as a finding names it.
func (l Listed) Name() string {
	if l.Pack == "" {
		return l.ID
	}
	return l.Pack + "/" + l.ID
}
