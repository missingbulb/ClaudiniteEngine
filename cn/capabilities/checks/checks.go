package checks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/builtin"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/run"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/sdkserver"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// Service runs the declared packs' coded checks for one engine.
type Service struct {
	Build build.Config
	// Caller names a foreground build's wait, as the event names a run's.
	Caller string
	// Timing receives the build's breadcrumbs; nil drops them.
	Timing func(line string)
}

// Key is the repo's checks binary key and its sources; "" when no active
// canon or local pack has Go checks, or the repo has no settings file at
// all. A temp pack's checks are never built: they are a per-session copy
// no review has seen.
func (s Service) Key(repo string) (string, []build.Source, error) {
	if !hasSettings(repo) {
		return "", nil, nil
	}
	set, err := packset.Load(repo, s.Build.Engine, false)
	if err != nil {
		return "", nil, err
	}
	srcs, err := build.Sources(set.Packs)
	if err != nil {
		return "", nil, err
	}
	return build.Key(s.Build, srcs), srcs, nil
}

func (s Service) note(line string) {
	if s.Timing != nil {
		s.Timing(line)
	}
}

// noteWait reports a wait for the binary by event that began at began,
// ended by err.
func (s Service) noteWait(event string, began time.Time, err error) {
	outcome := breadcrumb.OK
	switch {
	case errors.Is(err, build.ErrTimeout):
		outcome = breadcrumb.Timeout
	case err != nil:
		outcome = breadcrumb.Error
	}
	s.note(breadcrumb.Line("buildwait", event, outcome, time.Since(began)))
}

// noteCompile reports the compile of key this process ran, from its
// record.
func (s Service) noteCompile(key string) {
	rec, ok := build.ReadRecord(s.Build, key)
	if !ok {
		return
	}
	outcome := breadcrumb.OK
	if !rec.OK {
		outcome = breadcrumb.Error
	}
	s.note(breadcrumb.Line("build", "compiled", outcome, rec.Took))
}

// BuildNow builds the repo's checks binary in this process; when another
// process is building it, wait says whether to wait for that build, for
// up to timeout. It returns the key, "" for nothing to build. A binary
// not there on arrival is a wait, reported under Caller.
func (s Service) BuildNow(repo, wantKey string, wait bool, timeout time.Duration) (string, error) {
	key, srcs, err := s.Key(repo)
	if err != nil {
		return "", err
	}
	if wantKey != "" && wantKey != key {
		return "", fmt.Errorf("the packs changed since key %s was computed (now %s)", wantKey, key)
	}
	_, err = s.ensure(key, srcs, s.Caller, wait, timeout)
	return key, err
}

// ensure builds key's binary here unless it is built, waiting up to
// timeout for another process's build of it when wait says to, and
// returns the binary. A binary not there on arrival is a wait, reported
// under event when event is set.
func (s Service) ensure(key string, srcs []build.Source, event string, wait bool, timeout time.Duration) (string, error) {
	if key == "" {
		return "", nil
	}
	if binary, err := build.Wait(s.Build, key, 0); err == nil {
		return binary, nil
	}
	began := time.Now()
	err := build.Build(s.Build, key, srcs)
	switch {
	case errors.Is(err, build.ErrBuilding) && wait:
		_, err = build.Wait(s.Build, key, timeout)
	case !errors.Is(err, build.ErrBuilding):
		s.noteCompile(key)
	}
	if event != "" {
		s.noteWait(event, began, err)
	}
	if err != nil {
		return "", err
	}
	return s.Build.Binary(key), nil
}

// prepared is the checks binary a run will use, or why it has none.
type prepared struct {
	key    string
	binary string
	err    error
}

// prepare builds the repo's checks binary here unless it is built, as
// CI does; a build another process holds is waited for up to wait.
func (s Service) prepare(repo, event string, wait time.Duration) prepared {
	key, srcs, err := s.Key(repo)
	if err != nil || key == "" {
		return prepared{key: key, err: err}
	}
	binary, err := s.ensure(key, srcs, event, true, wait)
	return prepared{key: key, binary: binary, err: err}
}

// Prepare builds the repo's checks binary here unless it is built, so a
// guard judges with every coded judge; a build another process holds is
// waited for up to wait, and a build of these sources that already failed
// is not run again. The wait is reported under event.
func (s Service) Prepare(repo, event string, wait time.Duration) error {
	key, srcs, err := s.Key(repo)
	if err != nil || key == "" || build.Failed(s.Build, key) {
		return err
	}
	_, err = s.ensure(key, srcs, event, true, wait)
	return err
}

// Run runs the checks whose tags include every one of tags (from pack when
// set) over repo, srv answering their SDK calls, building the binary here
// first unless it is built. The breadcrumb records the outcome: a binary
// not ready in time is a timeout, never silence. A repo with no Go checks
// runs nothing, ok.
func (s Service) Run(repo, event string, tags []string, pack string, wait time.Duration, srv run.Server) (run.Result, string) {
	start := time.Now()
	return s.runPrepared(s.prepare(repo, event, wait), event, tags, pack, repo, srv, start)
}

func (s Service) runPrepared(p prepared, event string, tags []string, pack, repo string, srv run.Server, start time.Time) (run.Result, string) {
	if p.err == nil && p.key == "" {
		return run.Result{}, breadcrumb.Line("checks", event, breadcrumb.OK, time.Since(start))
	}
	if p.err != nil {
		outcome := breadcrumb.Error
		if errors.Is(p.err, build.ErrTimeout) {
			outcome = breadcrumb.Timeout
		}
		return run.Result{Err: p.err}, breadcrumb.Line("checks", event, outcome, time.Since(start))
	}
	res, _ := run.Runner{Binary: p.binary, Engine: s.Build.Engine, Server: srv}.Run(event, tags, pack, repo)
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

// ErrNotBuilt is a listing that would have had to build the checks
// binary.
var ErrNotBuilt = errors.New("the coded checks are not built for the current packs")

// ListBuilt returns the repo's coded checks from a binary already built
// for the current packs, ErrNotBuilt when there is none; it never builds.
func (s Service) ListBuilt(repo string) ([]run.Listed, error) {
	key, _, err := s.Key(repo)
	if err != nil || key == "" {
		return nil, err
	}
	binary, err := build.Wait(s.Build, key, 0)
	if err != nil {
		return nil, ErrNotBuilt
	}
	return run.Runner{Binary: binary, Engine: s.Build.Engine}.List()
}

// LoadSet reads the repo's declared checks with the engine's own
// built-ins, each where its pack is declared.
func (s Service) LoadSet(repo string) (*declared.Set, error) {
	return declared.LoadSet(repo, s.Build.Engine, builtin.All()...)
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
	// Crumb is the checks breadcrumb; DeclaredCrumb the declared run's;
	// SDKCrumb the coded child's SDK calls', "" when no child ran.
	Crumb, DeclaredCrumb, SDKCrumb string
	// Calls counts the coded child's SDK calls by method.
	Calls map[string]int
	// Stderr is the tail of the coded child's stderr.
	Stderr string
	// Skipped are the declared and built-in checks a git fault kept from
	// running.
	Skipped []string
}

// Blocking reports whether any finding blocks.
func (o Outcome) Blocking() bool { return findings.AnyBreak(o.Findings) }

// RunAll runs the declared checks sel takes in this process while the
// coded ones' binary is built beside them, then the coded ones with the
// same tags and pack as Run does, their SDK calls answered from the
// declared run's own walk and session; applies grace to the coded
// findings, then the member's checks configuration over both.
func (s Service) RunAll(repo, event string, sel declared.Selection, wait time.Duration, stderr io.Writer) Outcome {
	tags, pack := sel.Tags, sel.Pack
	start := time.Now()
	built := make(chan prepared, 1)
	go func() { built <- s.prepare(repo, event, wait) }()
	var out Outcome
	set, err := s.LoadSet(repo)
	var cfg declared.Config
	n := 0
	if err != nil {
		out.Findings = append(out.Findings, findings.Finding{Class: findings.Break, ID: "checks-run", Path: ".claudinite", Sentence: "the declared checks could not load: " + err.Error()})
		set = &declared.Set{Repo: repo}
	} else {
		var fs []findings.Finding
		fs, n = set.Run(sel, start, stderr)
		out.Findings = append(out.Findings, fs...)
		out.Skipped = set.Skipped
		cfg = set.Config
	}
	out.DeclaredCrumb = fmt.Sprintf("[cn] declared %d checks %dms", n, time.Since(start).Milliseconds())
	srv := newServer(repo, set, sel.Session, start)
	res, crumb := s.runPrepared(<-built, event, tags, pack, repo, srv, start)
	out.Crumb, out.Errors, out.Err, out.SDKCrumb, out.Calls, out.Stderr = crumb, res.Errors, res.Err, res.SDKCrumb, res.Calls, res.Stderr
	for i, f := range res.Shared() {
		out.Findings = append(out.Findings, declared.Grace(f, res.Findings[i].Since, start))
	}
	out.Findings = declared.ApplyConfig(out.Findings, cfg)
	return out
}

// lazyServer answers the coded checks' SDK calls, walking the tree only
// on the first call that needs it.
type lazyServer struct {
	repo    string
	set     *declared.Set
	session *transcript.Session
	now     time.Time
	srv     *sdkserver.Server
}

func newServer(repo string, set *declared.Set, session *transcript.Session, now time.Time) *lazyServer {
	return &lazyServer{repo: repo, set: set, session: session, now: now}
}

func (l *lazyServer) server() *sdkserver.Server {
	if l.srv == nil {
		cfg := sdkserver.Config{PackConfig: l.set.Config.PackConfig, Rules: l.set.Config.Rules, Packs: l.set.Packs}
		for _, a := range l.set.Config.Accept {
			cfg.Accept = append(cfg.Accept, sdkserver.Acceptance{Rule: a.Rule, Path: a.Path, Reason: a.Reason, Pack: a.Pack})
		}
		l.srv = sdkserver.Serve(l.repo, l.set.Context(l.now), l.session, cfg)
	}
	return l.srv
}

func (l *lazyServer) Methods() []string { return sdkserver.Methods() }

func (l *lazyServer) Handle(method string, args json.RawMessage) (json.RawMessage, error) {
	return l.server().Handle(method, args)
}

// ListAll names the declared, built-in and coded checks, sorted by id,
// building the coded checks' binary here.
func (s Service) ListAll(repo string, timeout time.Duration) ([]Listed, error) {
	return s.listWith(repo, func() ([]run.Listed, error) { return s.List(repo, timeout) })
}

// ListAllBuilt is ListAll with the coded checks from ListBuilt: it never
// builds, and with no binary it returns the declared and built-in checks
// and ErrNotBuilt.
//
// Both return the declared and built-in checks beside an error listing
// the coded ones.
func (s Service) ListAllBuilt(repo string) ([]Listed, error) {
	return s.listWith(repo, func() ([]run.Listed, error) { return s.ListBuilt(repo) })
}

func (s Service) listWith(repo string, coded func() ([]run.Listed, error)) ([]Listed, error) {
	var out []Listed
	set, err := s.LoadSet(repo)
	if err != nil {
		return nil, err
	}
	for _, l := range set.List() {
		out = append(out, Listed{ID: l.ID, Pack: l.Pack, Kind: l.Kind, Tags: l.Tags, OnFail: l.OnFail, Since: l.Since})
	}
	listed, codedErr := coded()
	for _, c := range listed {
		pack, id := splitName(c.Check)
		kind := "coded"
		if c.Judge {
			kind = "judge"
		}
		onFail := c.OnFail
		if onFail == "" {
			onFail = "block"
		}
		out = append(out, Listed{ID: id, Pack: pack, Kind: kind, Tags: c.Tags, OnFail: onFail, Since: c.Since})
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].ID != out[k].ID {
			return out[i].ID < out[k].ID
		}
		return out[i].Pack < out[k].Pack
	})
	return out, codedErr
}

// Listed is one check as cn check list prints it.
type Listed struct {
	ID, Pack, Kind string
	Tags           []string
	OnFail, Since  string
}

// splitName splits a coded check's name, <pack>/<id> or
// local/<name>/<id>, at its last slash.
func splitName(name string) (pack, id string) {
	i := strings.LastIndex(name, "/")
	if i < 0 {
		return "", name
	}
	return name[:i], name[i+1:]
}

// Name is the check as a finding names it.
func (l Listed) Name() string {
	if l.Pack == "" {
		return l.ID
	}
	return l.Pack + "/" + l.ID
}
