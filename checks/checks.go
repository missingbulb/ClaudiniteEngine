package checks

import (
	"errors"
	"fmt"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/checks/run"
	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// Service runs the declared packs' coded checks for one engine.
type Service struct {
	Build build.Config
	// Exe is this cn, which Start runs detached as `cn check build`.
	Exe string
}

// Key is the repo's checks binary key and its sources; "" when no declared
// pack has Go checks.
func (s Service) Key(repo string) (string, []build.Source, error) {
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
