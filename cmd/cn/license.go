package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/hooks"
	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/adopt"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// licenseEnv is the session license flow over the real clients. log
// receives the background request's progress.
func licenseEnv(log io.Writer) (license.Env, error) {
	roots, err := license.Roots()
	if err != nil {
		return license.Env{}, err
	}
	return license.Env{
		CacheRoot: paths.CacheRoot(),
		Roots:     roots,
		Engine:    version.Version(),
		Now:       time.Now,
		Sleep:     time.Sleep,
		Getenv:    os.Getenv,
		GitHub: func(repo, token string, timeout time.Duration) license.GitHub {
			c := githubapi.SessionFromEnv(repo)
			if token != "" {
				c.Token = token
			}
			c.HTTP = &http.Client{Timeout: timeout}
			return c
		},
		Worker: func() (license.Worker, error) { return licenseapi.FromEnv() },
		Origin: originOf,
		Plan:   planOf,
		Start:  startRequest,
		Log:    log,
	}, nil
}

// originOf is the checkout's configured origin URL, unrewritten.
func originOf(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return "", errors.New("the checkout has no origin remote")
	}
	return strings.TrimSpace(string(out)), nil
}

// planOf is the settings file's license.plan; "" when there is none.
func planOf(dir string) string {
	path, f, err := settings.Find(dir)
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	l, err := settings.ReadLicense(raw, f)
	if err != nil {
		return ""
	}
	return l.Plan
}

// startRequest runs `cn license request` detached, its stderr appended to
// <cache>/sessions/<id>.log.
func startRequest(sessionID, nonce, dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logDir := license.SessionsDir(paths.CacheRoot())
	if err := paths.EnsurePrivateDir(logDir); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(logDir, sessionID+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logf.Close() }()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "license", "request", "--session", sessionID, "--nonce", nonce, "--repo", abs)
	cmd.SysProcAttr = build.Detached()
	cmd.Dir = os.TempDir()
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// hookLicense gives the hooks the session license flow.
type hookLicense struct{}

func (hookLicense) env() (license.Env, bool) {
	e, err := licenseEnv(nil)
	return e, err == nil
}

func (h hookLicense) SessionStart(repo, sessionID string) hooks.LicenseStatus {
	e, ok := h.env()
	if !ok {
		return hooks.LicenseStatus{Line: "[cn] license: no embedded roots", State: "degraded"}
	}
	return toHook(e.SessionStart(repo, sessionID))
}

func (h hookLicense) Hook(repo, sessionID string) hooks.LicenseStatus {
	e, ok := h.env()
	if !ok {
		return hooks.LicenseStatus{State: "degraded"}
	}
	return toHook(e.Hook(repo, sessionID))
}

func toHook(s license.Status) hooks.LicenseStatus {
	return hooks.LicenseStatus{Line: s.Line, Notice: s.Notice, WorkChecks: s.Gates.On(license.SurfaceWorkChecks),
		State: stateName(s.Verdict), Crumbs: s.Crumbs}
}

func stateName(v license.Verdict) string {
	switch {
	case v.Key != nil:
		return v.Key.State
	case v.Pending:
		return license.StatePending
	}
	return license.StateDegraded + ": " + string(v.Cause)
}

func cmdLicense(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "license takes status or request")
	}
	fs := flag.NewFlagSet("license", flag.ContinueOnError)
	session := fs.String("session", "", "")
	nonce := fs.String("nonce", "", "")
	repo := fs.String("repo", ".", "")
	if err := flags(fs, args[1:]); err != nil {
		return err
	}
	abs, err := filepath.Abs(*repo)
	if err != nil {
		return report.Wrap(report.IO, "license", err)
	}
	switch args[0] {
	case "request":
		return licenseRequest(*session, *nonce, abs, stdout, stderr)
	case "status":
		return licenseStatus(*session, abs, stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown license command %q", args[0]))
}

// licenseRequest is the background child with --nonce; without it, a
// foreground request for the session that waits out the request and then
// prints the status.
func licenseRequest(session, nonce, repo string, stdout, stderr io.Writer) error {
	if session == "" {
		return report.New(report.Usage, "license request needs --session ID")
	}
	e, err := licenseEnv(stderr)
	if err != nil {
		return report.Wrap(report.Internal, "license request", err)
	}
	if nonce != "" {
		if err := e.RunRequest(session, nonce, repo); err != nil {
			return report.Wrap(report.IO, "license request", err)
		}
		return nil
	}
	e.Start = func(id, n, dir string) error { return e.RunRequest(id, n, dir) }
	path, err := license.StatePath(e.CacheRoot, session)
	if err != nil {
		return report.Wrap(report.Usage, "license request", err)
	}
	_ = os.Remove(path)
	e.SessionStart(repo, session)
	return licenseStatus(session, repo, stdout)
}

// licenseStatus prints a session's state file and what a hook makes of it;
// with no --session, the newest session that ran in repo.
func licenseStatus(session, repo string, stdout io.Writer) error {
	e, err := licenseEnv(nil)
	if err != nil {
		return report.Wrap(report.Internal, "license status", err)
	}
	var path string
	if session != "" {
		if path, err = license.StatePath(e.CacheRoot, session); err != nil {
			return report.Wrap(report.Usage, "license status", err)
		}
	} else {
		path = newestSession(e.CacheRoot, repo)
	}
	f, err := license.ReadState(path)
	if err != nil {
		return report.Wrap(report.IO, "license status", err)
	}
	if path == "" || f == nil {
		fmt.Fprintln(stdout, "no session state for this repo")
		return nil
	}
	v := license.Decide(f, e.Now(), e.Roots)
	fmt.Fprintf(stdout, "session: %s\nrepo: %s\nmode: %s\nstate: %s\n", f.SessionID, f.Repo, f.Mode, stateName(v))
	if f.Plan != "" {
		fmt.Fprintf(stdout, "plan setting: %s\n", f.Plan)
	}
	if v.Key != nil {
		fmt.Fprintf(stdout, "key: %s plan, %s, landed by %s, expires %s\n", v.Key.Plan, v.Key.State, f.Path, time.Unix(v.Key.Exp, 0).UTC().Format(time.RFC3339))
	}
	if f.Dispatched {
		fmt.Fprintf(stdout, "dispatched at head: %s\n", f.Head)
	}
	if f.Cause != "" {
		fmt.Fprintf(stdout, "cause: %s\n", f.Cause)
	}
	if f.CauseDetail != "" {
		fmt.Fprintf(stdout, "detail: %s\n", f.CauseDetail)
	}
	if line := strings.TrimSpace(licenseLine(v)); line != "" {
		fmt.Fprintln(stdout, line)
	}
	var off []string
	g := license.Gate(v.Key, v.Pending)
	for _, s := range license.Surfaces {
		if !g.On(s) {
			off = append(off, string(s))
		}
	}
	if len(off) == 0 {
		fmt.Fprintln(stdout, "off: nothing")
	} else {
		fmt.Fprintf(stdout, "off: %s\n", strings.Join(off, ", "))
	}
	return nil
}

func licenseLine(v license.Verdict) string {
	switch {
	case v.Key != nil:
		return license.NoticeFor(v.Key, "", "", "")
	case v.Pending:
		return ""
	}
	return license.NoticeFor(nil, v.Cause, v.Detail, v.Link)
}

// newestSession is the newest state file whose checkout is repo; "" when
// none is.
func newestSession(cacheRoot, repo string) string {
	entries, err := os.ReadDir(license.SessionsDir(cacheRoot))
	if err != nil {
		return ""
	}
	type cand struct {
		path string
		at   time.Time
	}
	var cands []cand
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		p := filepath.Join(license.SessionsDir(cacheRoot), en.Name())
		f, err := license.ReadState(p)
		if err != nil || f == nil || filepath.Clean(f.Dir) != filepath.Clean(repo) {
			continue
		}
		cands = append(cands, cand{p, f.RequestedAt})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].at.After(cands[j].at) })
	return cands[0].path
}

// cmdLogin runs GitHub's device flow for the Claudinite App, with the
// client id and endpoints the license server names, and stores the App
// user token for desktop key requests.
func cmdLogin(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	logout := fs.Bool("logout", false, "")
	force := fs.Bool("force", false, "")
	if err := flags(fs, args); err != nil {
		return err
	}
	st := license.Store{Root: paths.CacheRoot()}
	if *logout {
		if err := st.DeleteLogin(); err != nil {
			return report.Wrap(report.IO, "login", err)
		}
		fmt.Fprintln(stdout, "logged out")
		return nil
	}
	if l, err := st.ReadLogin(); err == nil && l != nil && !*force && l.Valid(time.Now()) {
		fmt.Fprintf(stdout, "logged in as %s (cn login --force to log in again)\n", l.UserLogin)
		return nil
	}
	w, err := licenseapi.FromEnv()
	if err != nil {
		return report.Wrap(report.IO, "login", err)
	}
	cfg, err := w.LoginConfig()
	if err != nil {
		return report.Wrap(report.IO, "login", err)
	}
	h := &http.Client{Timeout: 10 * time.Second}
	d, err := githubapi.DeviceCode(h, cfg.DeviceCodeURL, cfg.ClientID)
	if err != nil {
		return report.Wrap(report.IO, "login", err)
	}
	fmt.Fprintf(stdout, "open %s and enter %s\n", d.VerificationURI, d.UserCode)
	interval := time.Duration(max(d.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(d.ExpiresIn, 60)) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		t, err := githubapi.DeviceToken(h, cfg.TokenURL, cfg.ClientID, d.DeviceCode)
		switch t.Error {
		case "":
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		default:
			return report.New(report.IO, "login: GitHub answered "+t.Error)
		}
		if err != nil {
			return report.Wrap(report.IO, "login", err)
		}
		c := githubapi.SessionFromEnv("")
		c.Token = t.AccessToken
		u, err := c.User()
		if err != nil {
			return report.Wrap(report.IO, "login", err)
		}
		l := license.Login{AccessToken: t.AccessToken, ExpiresIn: t.ExpiresIn, RefreshToken: t.RefreshToken,
			RefreshTokenExpiresIn: t.RefreshTokenExpiresIn, ObtainedAt: time.Now(), UserID: u.ID, UserLogin: u.Login}
		if err := st.WriteLogin(l); err != nil {
			return report.Wrap(report.IO, "login", err)
		}
		fmt.Fprintf(stdout, "logged in as %s\n", u.Login)
		return nil
	}
	return report.New(report.IO, "login: the device code expired")
}

// initKey is cn init's one key request: the session flow run in the
// foreground for the cut, its request in this process.
func initKey(stderr io.Writer) func(repo string) adopt.KeyGrant {
	return func(repo string) adopt.KeyGrant {
		e, err := licenseEnv(stderr)
		if err != nil {
			return adopt.KeyGrant{Reason: err.Error()}
		}
		if _, err := originOf(repo); err != nil {
			return adopt.KeyGrant{Reason: "the repo has no origin remote yet", Link: license.InstallURL}
		}
		e.Start = func(id, n, dir string) error {
			go func() { _ = e.RunRequest(id, n, dir) }()
			return nil
		}
		id := "init-" + license.NewNonce()
		path, err := license.StatePath(e.CacheRoot, id)
		if err != nil {
			return adopt.KeyGrant{Reason: err.Error()}
		}
		defer func() { _ = os.Remove(path) }()
		start := time.Now()
		e.SessionStart(repo, id)
		for {
			f, _ := license.ReadState(path)
			v := license.Decide(f, time.Now(), e.Roots)
			switch {
			case v.Key != nil:
				return adopt.KeyGrant{Plan: string(v.Key.Plan)}
			case f != nil && f.State == license.StateDegraded && f.Cause != "":
				return adopt.KeyGrant{Reason: string(f.Cause), Link: license.LinkFor(f.Cause)}
			case time.Since(start) >= license.Cut:
				c := license.CauseAppNotInstalled
				if f != nil && !f.Dispatched {
					c = license.CauseGitHubUnreachable
				}
				return adopt.KeyGrant{Reason: string(c), Link: license.InstallURL}
			}
			time.Sleep(license.PollEvery / 5)
		}
	}
}
