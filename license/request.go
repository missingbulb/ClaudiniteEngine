package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/breadcrumb"
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
)

// CheckRunName is the check run a web key travels in.
const CheckRunName = "Claudinite key"

// PollEvery is how often the background request reads the check runs.
const PollEvery = 500 * time.Millisecond

// HookPollBudget bounds a hook's own read while a key is pending.
const HookPollBudget = 2 * time.Second

// GitHub is the session's GitHub surface.
type GitHub interface {
	User() (githubapi.Account, error)
	RepoInfo() (githubapi.RepoInfo, error)
	BranchHead(branch string) (string, error)
	RepositoryDispatch(eventType string, payload any) error
	CheckRuns(sha, name string) ([]githubapi.CheckRun, error)
}

// Worker is the license server's surface.
type Worker interface {
	Refresh(refreshToken string) (licenseapi.TokenAnswer, error)
	SessionKey(userToken, repo, nonce, engine string) (licenseapi.KeyAnswer, error)
	PublicSessionKey(userToken, repo, nonce, engine string) (licenseapi.KeyAnswer, error)
	ActionsKey(oidcToken, engine string) (licenseapi.KeyAnswer, error)
}

// Env is everything a session's license flow reads and writes; cmd/cn
// supplies the real ones.
type Env struct {
	CacheRoot string
	Roots     []ed25519.PublicKey
	Engine    string
	Now       func() time.Time
	Sleep     func(time.Duration)
	Getenv    func(string) string
	// GitHub is a client for repo (owner/name), with token when one is
	// given and the environment's otherwise.
	GitHub func(repo, token string, timeout time.Duration) GitHub
	// Worker is the license server's client.
	Worker func(timeout time.Duration) (Worker, error)
	// Origin is the checkout's origin URL, as configured.
	Origin func(dir string) (string, error)
	// Plan is the settings file's license.plan, "" when none.
	Plan func(dir string) string
	// Start runs the background request detached and returns at once.
	Start func(sessionID, nonce, dir string) error
	// Log receives the background request's progress.
	Log io.Writer
}

func (e Env) store() Store { return Store{Root: e.CacheRoot} }

func (e Env) logf(format string, a ...any) {
	if e.Log != nil {
		fmt.Fprintf(e.Log, format+"\n", a...)
	}
}

// NewNonce is a fresh session nonce, 22 base64url characters.
func NewNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Status is what a hook applies: the gate, the line SessionStart shows,
// and a notice to pass on (once per distinct notice).
type Status struct {
	Gates   Gates
	Verdict Verdict
	Line    string
	Notice  string
	// Crumbs are license breadcrumbs this hook observed.
	Crumbs []string
}

func (e Env) line(v Verdict, f *File) string {
	switch {
	case v.Key != nil:
		l := fmt.Sprintf("[cn] license ok: %s, %s", v.Key.Plan, v.Key.State)
		if n := NoticeFor(v.Key, "", "", ""); n != "" {
			l += "\n" + n
		}
		return l
	case v.Pending:
		return "[cn] license pending: every feature is on while the key arrives"
	}
	return NoticeFor(nil, v.Cause, v.Detail, v.Link)
}

// statusOf gates a verdict.
func (e Env) statusOf(v Verdict, f *File) Status {
	return Status{Gates: Gate(v.Key, v.Pending), Verdict: v, Line: e.line(v, f)}
}

// SessionStart reads the session's state file and never waits: a landed
// key under a day old applies at once (a resume finds its file); a
// request still in flight is left to finish; otherwise it writes pending
// with a fresh nonce and starts the background request.
func (e Env) SessionStart(dir, sessionID string) Status {
	now := e.Now()
	path, err := StatePath(e.CacheRoot, sessionID)
	if err != nil {
		return e.statusOf(Verdict{Cause: CauseStateFile, Detail: err.Error()}, nil)
	}
	f, err := ReadState(path)
	if err != nil {
		f = nil
	}
	if f != nil {
		v := Decide(f, now, e.Roots)
		switch {
		case v.Key != nil && v.Renew:
			e.renew(path, f, dir)
			return e.markNoticed(path, e.statusOf(v, f))
		case v.Key != nil, v.Pending, f.State == StatePending && !v.TailOver:
			return e.markNoticed(path, e.statusOf(v, f))
		}
	}
	nf := &File{SessionID: sessionID, Dir: dir, Nonce: NewNonce(), RequestedAt: now, State: StatePending, Mode: PathWeb}
	if e.Plan != nil {
		nf.Plan = e.Plan(dir)
	}
	if e.Getenv("GITHUB_ACTIONS") == "true" {
		nf.State, nf.Cause = StateDegraded, CauseActions
	} else if login, _ := e.store().ReadLogin(); login != nil {
		nf.Mode = PathDesktop
	}
	if nf.State == StatePending {
		origin, err := e.Origin(dir)
		repo, ok := githubapi.ParseRemote(origin)
		if err != nil || !ok {
			nf.State, nf.Cause, nf.CauseDetail = StateDegraded, CauseNoGitHubRemote, strings.TrimSpace(origin)
		}
		nf.Repo = repo
	}
	if err := UpdateState(path, func(*File) (*File, bool) { return nf, true }); err != nil {
		return e.statusOf(Verdict{Cause: CauseStateFile, Detail: err.Error()}, nil)
	}
	if nf.State == StatePending {
		if err := e.Start(sessionID, nf.Nonce, dir); err != nil {
			_ = UpdateState(path, func(f *File) (*File, bool) {
				if f == nil || f.Nonce != nf.Nonce {
					return nil, false
				}
				f.State, f.Cause, f.CauseDetail = StateDegraded, CauseStateFile, "the key request did not start: "+err.Error()
				return f, true
			})
		}
	}
	f, _ = ReadState(path)
	return e.markNoticed(path, e.statusOf(Decide(f, e.Now(), e.Roots), f))
}

// renew starts a fresh request beside a landed key, which stays in use
// until the new one lands.
func (e Env) renew(path string, f *File, dir string) {
	nonce := NewNonce()
	started := false
	_ = UpdateState(path, func(cur *File) (*File, bool) {
		if cur == nil || cur.Key != f.Key {
			return nil, false
		}
		cur.Nonce, cur.RequestedAt, cur.Dispatched, cur.Head = nonce, e.Now(), false, ""
		cur.RenewAttempts++
		started = true
		return cur, true
	})
	if started {
		_ = e.Start(f.SessionID, nonce, dir)
	}
}

// markNoticed records s's notice as passed on, so a later hook does not
// repeat it.
func (e Env) markNoticed(path string, s Status) Status {
	n := s.Line
	if s.Verdict.Pending || (s.Verdict.Key != nil && NoticeFor(s.Verdict.Key, "", "", "") == "") {
		n = ""
	}
	_ = UpdateState(path, func(f *File) (*File, bool) {
		if f == nil || f.Noticed == n {
			return nil, false
		}
		f.Noticed = n
		return f, true
	})
	return s
}

// Hook is every later hook's read: the state file, decided at now, with
// at most one bounded poll of its own while a requested key is pending,
// the cut written into the file when it has passed, a renewal started
// when the key is a day old, and the notice once.
func (e Env) Hook(dir, sessionID string) Status {
	path, err := StatePath(e.CacheRoot, sessionID)
	if err != nil {
		return e.statusOf(Verdict{Cause: CauseStateFile, Detail: err.Error()}, nil)
	}
	f, err := ReadState(path)
	if err != nil {
		v := Verdict{Cause: CauseStateFile, Detail: err.Error()}
		s := e.statusOf(v, nil)
		s.Notice = s.Line
		return s
	}
	v := Decide(f, e.Now(), e.Roots)
	if f == nil {
		// No SessionStart wrote this session's file: nothing to poll and
		// nowhere to remember a notice, so none is passed on.
		return e.statusOf(v, nil)
	}
	var crumbs []string
	if v.Key == nil && v.PollDue {
		start := e.Now()
		if e.pollOnce(path, f, PathHookPoll, HookPollBudget) {
			crumbs = append(crumbs, breadcrumb.Line("license", "landed", breadcrumb.OK, e.Now().Sub(start)))
			f, _ = ReadState(path)
			v = Decide(f, e.Now(), e.Roots)
		}
	}
	if v.CutPassed {
		cause := v.Cause
		_ = UpdateState(path, func(cur *File) (*File, bool) {
			if cur == nil || cur.State != StatePending || cur.Nonce != f.Nonce || cur.Key != "" {
				return nil, false
			}
			cur.State, cur.Cause, cur.Link = StateDegraded, cause, LinkFor(cause)
			return cur, true
		})
		crumbs = append(crumbs, breadcrumb.Line("license", "cut", breadcrumb.Timeout, e.Now().Sub(f.RequestedAt)))
	}
	if v.Renew {
		e.renew(path, f, dir)
		crumbs = append(crumbs, breadcrumb.Line("license", "renew", breadcrumb.OK, 0))
	}
	s := e.statusOf(v, f)
	s.Crumbs = crumbs
	notice := ""
	if !v.Pending {
		if v.Key != nil {
			notice = NoticeFor(v.Key, "", "", "")
		} else {
			notice = s.Line
		}
	}
	if notice != "" && notice != f.Noticed {
		shown := false
		_ = UpdateState(path, func(cur *File) (*File, bool) {
			if cur == nil || cur.Noticed == notice {
				return nil, false
			}
			cur.Noticed, shown = notice, true
			return cur, true
		})
		if shown {
			s.Notice = notice
		}
	}
	if v.Key != nil && f.Noticed != "" && notice == "" {
		_ = UpdateState(path, func(cur *File) (*File, bool) {
			if cur == nil || cur.Noticed == "" {
				return nil, false
			}
			cur.Noticed = ""
			return cur, true
		})
	}
	return s
}

// pollOnce makes one read for a pending key (the check runs on the web,
// the one Worker call on a desktop) and lands what it finds; it reports
// whether a key landed.
func (e Env) pollOnce(path string, f *File, landedBy string, timeout time.Duration) bool {
	if f.Mode == PathDesktop {
		login, err := e.store().ReadLogin()
		if err != nil || login == nil || f.Identity == nil {
			return false
		}
		origin, _ := e.Origin(f.Dir)
		return e.askWorker(path, f, *login, origin, landedBy, timeout)
	}
	gh := e.GitHub(f.Repo, "", timeout)
	done, landed, _ := e.readCheckRun(path, f, gh, landedBy)
	return done && landed
}

// readCheckRun reads the check runs on f.Head once: a completed run whose
// external id is the nonce either carries the key, which lands, or a
// refusal, which degrades. A key for another user is another request
// that reused the nonce, and is passed over, which stranger reports.
// done reports that the request is answered.
func (e Env) readCheckRun(path string, f *File, gh GitHub, landedBy string) (done, landed, stranger bool) {
	runs, err := gh.CheckRuns(f.Head, CheckRunName)
	if err != nil {
		e.logf("check runs: %v", err)
		return false, false, false
	}
	for _, r := range runs {
		if r.ExternalID != f.Nonce || r.Status != "completed" {
			continue
		}
		e.logf("check run %d (%s, app %s): %s", r.ID, r.Output.Title, r.App.Slug, r.Output.Summary)
		switch r.Output.Title {
		case CheckRunName:
			if k, err := VerifyKey([]byte(r.Output.Text), e.Roots, e.Now()); err == nil && k.UserID != nil && *k.UserID != f.UserID {
				e.logf("check run %d: a key for user %d, not %d; passed over", r.ID, *k.UserID, f.UserID)
				stranger = true
				continue
			}
			return true, e.land(path, f, []byte(r.Output.Text), landedBy), stranger
		case CheckRunName + " refused":
			reason, detail := refusal(r.Output.Summary)
			e.degrade(path, f, reason, detail)
			return true, false, stranger
		}
	}
	return false, false, stranger
}

var refusalPrefix = regexp.MustCompile(`^([a-z0-9-]+):\s*(.*)$`)

// refusal is a refused check run's cause and detail, from its summary's
// `<reason>: <text>`. A summary with no such prefix is the public
// Worker's private-repo refusal as it was written before it had one.
func refusal(summary string) (Cause, string) {
	summary = strings.TrimSpace(summary)
	if m := refusalPrefix.FindStringSubmatch(summary); m != nil {
		return Cause(m[1]), strings.TrimSpace(m[2])
	}
	return CauseRefusedPrivate, summary
}

// land verifies and binds a key fetched for f's request and writes it;
// a key that fails either degrades instead. It reports whether it landed.
func (e Env) land(path string, f *File, key []byte, landedBy string) bool {
	k, err := VerifyKey(key, e.Roots, e.Now())
	if err != nil {
		e.degrade(path, f, CauseKeyRefused, string(ReasonOf(err)))
		return false
	}
	if f.Identity == nil {
		e.degrade(path, f, CauseStateFile, "no repo identity")
		return false
	}
	if c := Bind(k, *f.Identity, &Session{UserID: f.UserID, Nonce: f.Nonce}); c != "" {
		e.degrade(path, f, c, bindDetail(c, k, *f.Identity))
		return false
	}
	return e.write(path, f, func(cur *File) bool {
		now := e.Now()
		cur.State, cur.Key, cur.KeyNonce, cur.LandedAt, cur.Path = StateLanded, string(key), f.Nonce, &now, landedBy
		cur.Cause, cur.CauseDetail, cur.Link = "", "", ""
		cur.RenewAttempts = 0
		return true
	})
}

// degrade writes a request's failure, unless the file has moved on to
// another request, or holds a key (a renewal keeps the old key in use).
func (e Env) degrade(path string, f *File, c Cause, detail string) {
	e.logf("degraded: %s %s", c, detail)
	e.write(path, f, func(cur *File) bool {
		if cur.Key != "" {
			return false
		}
		cur.State, cur.Cause, cur.CauseDetail, cur.Link = StateDegraded, c, detail, LinkFor(c)
		return true
	})
}

// degradeRefused records the server's refusal as the cause, with the
// checkout and portal links it named.
func (e Env) degradeRefused(path string, f *File, ref *licenseapi.Refusal) {
	c := Cause(ref.Reason)
	e.logf("degraded: %s (refused %d)", c, ref.Status)
	e.write(path, f, func(cur *File) bool {
		if cur.Key != "" {
			return false
		}
		cur.State, cur.Cause, cur.CauseDetail, cur.Link = StateDegraded, c, "", LinkFor(c)
		cur.Checkout, cur.Portal = ref.CheckoutURL, ref.PortalURL
		return true
	})
}

// write applies edit to the file under its lock while it still belongs to
// f's request (same nonce) and that request has not landed its key yet;
// edit returning false writes nothing. What the request observed (the
// user, the repo, the head, the dispatch) goes in with the edit.
func (e Env) write(path string, f *File, edit func(cur *File) bool) bool {
	wrote := false
	_ = UpdateState(path, func(cur *File) (*File, bool) {
		if cur == nil || cur.Nonce != f.Nonce || (cur.Key != "" && cur.KeyNonce == f.Nonce) {
			return nil, false
		}
		if !edit(cur) {
			return nil, false
		}
		if cur.Key == "" || cur.KeyNonce == f.Nonce {
			cur.UserID, cur.Identity = f.UserID, f.Identity
		}
		cur.Head, cur.Dispatched = f.Head, f.Dispatched
		wrote = true
		return cur, true
	})
	return wrote
}

// RunRequest is the background request, `cn license request`: the path
// the file names, written into the file as it goes, polling until the cut
// and then the tail for a late key.
func (e Env) RunRequest(sessionID, nonce string, dir string) error {
	start := e.Now()
	path, err := StatePath(e.CacheRoot, sessionID)
	if err != nil {
		return err
	}
	f, err := ReadState(path)
	if err != nil {
		return err
	}
	if f == nil || f.Nonce != nonce {
		return errors.New("the state file names another request")
	}
	event := "request-" + f.Mode
	outcome := breadcrumb.Error
	defer func() { e.logf("%s", breadcrumb.Line("license", event, outcome, e.Now().Sub(start))) }()
	if f.Mode == PathDesktop {
		if e.requestDesktop(path, f, dir) {
			outcome = breadcrumb.OK
		}
		return nil
	}
	gh := e.GitHub(f.Repo, "", githubapi.SessionTimeout)
	if !e.startWeb(path, f, gh) {
		return nil
	}
	cutWritten, strangers := false, false
	end := f.RequestedAt.Add(Cut + Tail())
	for e.Now().Before(end) {
		done, landed, stranger := e.readCheckRun(path, f, gh, PathWeb)
		strangers = strangers || stranger
		if done {
			if landed {
				outcome = breadcrumb.OK
			}
			return nil
		}
		cur, _ := ReadState(path)
		if cur == nil || cur.Nonce != nonce {
			return nil
		}
		if cur.Key != "" && cur.KeyNonce == nonce {
			outcome = breadcrumb.OK
			return nil
		}
		if !cutWritten && !e.Now().Before(f.RequestedAt.Add(Cut)) {
			cutWritten = true
			e.logf("%s", breadcrumb.Line("license", "cut", breadcrumb.Timeout, e.Now().Sub(start)))
			if strangers {
				e.degrade(path, f, CauseBindUser, "only a key for another user")
			} else {
				e.degrade(path, f, likeliest(f), "")
			}
		}
		e.Sleep(PollEvery)
	}
	outcome = breadcrumb.Timeout
	return nil
}

// startWeb reads the user, the repo and its default branch's head, and
// sends the dispatch, recording each in the file; it reports whether the
// dispatch went out.
func (e Env) startWeb(path string, f *File, gh GitHub) bool {
	u, err := gh.User()
	if err != nil {
		e.degrade(path, f, unreachableOr(err, CauseGitHubUser), err.Error())
		return false
	}
	if u.Type != "User" {
		e.degrade(path, f, CauseGitHubUser, "GET /user names a "+u.Type)
		return false
	}
	ri, err := gh.RepoInfo()
	if err != nil {
		e.degrade(path, f, unreachableOr(err, "repo-not-visible"), err.Error())
		return false
	}
	head, err := gh.BranchHead(ri.DefaultBranch)
	if err != nil {
		e.degrade(path, f, unreachableOr(err, "repo-not-visible"), err.Error())
		return false
	}
	f.UserID, f.Head = u.ID, head
	f.Identity = &RepoIdentity{ID: ri.ID, OwnerID: ri.Owner.ID, OwnerType: ri.Owner.Type, OwnerLogin: ri.Owner.Login, Private: ri.Private}
	event := "claudinite-key"
	if f.Plan == string(PlanPublic) {
		event = "claudinite-key-public"
	}
	err = gh.RepositoryDispatch(event, map[string]string{"nonce": f.Nonce, "engine_version": e.Engine, "head": head})
	switch st := githubapi.StatusOf(err); {
	case err == nil:
	case st == http.StatusForbidden || st == http.StatusNotFound:
		e.degrade(path, f, CauseNoPushAccess, err.Error())
		return false
	default:
		e.degrade(path, f, unreachableOr(err, "dispatch-refused"), err.Error())
		return false
	}
	f.Dispatched = true
	e.logf("dispatched %s for %s at %s (user %d, repo %d)", event, f.Repo, head, u.ID, ri.ID)
	e.write(path, f, func(cur *File) bool {
		return cur.Key == "" || cur.LandedAt == nil || f.RequestedAt.After(*cur.LandedAt)
	})
	return true
}

func unreachableOr(err error, c Cause) Cause {
	if githubapi.StatusOf(err) == 0 {
		return CauseGitHubUnreachable
	}
	return c
}

// requestDesktop asks the Worker directly with the login token: the user
// and the repo are read from GitHub with that same token for the binding,
// a 401 refreshes the token once, and an unreachable server falls back to
// the repo's cached key. It reports whether a key landed.
func (e Env) requestDesktop(path string, f *File, dir string) bool {
	st := e.store()
	login, err := st.ReadLogin()
	if err != nil || login == nil {
		e.degrade(path, f, CauseLoginExpired, "no login")
		return false
	}
	origin, _ := e.Origin(dir)
	gh := e.GitHub(f.Repo, login.AccessToken, githubapi.SessionTimeout)
	u, uerr := gh.User()
	var ri githubapi.RepoInfo
	if uerr == nil {
		ri, uerr = gh.RepoInfo()
	}
	if uerr != nil && githubapi.StatusOf(uerr) == http.StatusUnauthorized {
		if l, ok := e.refresh(*login, licenseapi.Timeout); ok {
			login = &l
			gh = e.GitHub(f.Repo, login.AccessToken, githubapi.SessionTimeout)
			if u, uerr = gh.User(); uerr == nil {
				ri, uerr = gh.RepoInfo()
			}
		} else {
			e.degrade(path, f, CauseLoginExpired, uerr.Error())
			return false
		}
	}
	if uerr != nil {
		if githubapi.StatusOf(uerr) == 0 && e.fromCache(path, f, origin, login.UserID) {
			return true
		}
		e.degrade(path, f, unreachableOr(uerr, "repo-not-visible"), uerr.Error())
		return false
	}
	f.UserID = u.ID
	f.Identity = &RepoIdentity{ID: ri.ID, OwnerID: ri.Owner.ID, OwnerType: ri.Owner.Type, OwnerLogin: ri.Owner.Login, Private: ri.Private}
	e.write(path, f, func(cur *File) bool { return true })
	return e.askWorker(path, f, *login, origin, PathDesktop, licenseapi.Timeout)
}

// askWorker makes the desktop's one Worker call for f's request and lands
// its key, refreshing a rejected token once and falling back to the cache
// when the server cannot be reached.
func (e Env) askWorker(path string, f *File, login Login, origin, landedBy string, timeout time.Duration) bool {
	st := e.store()
	w, err := e.Worker(timeout)
	if err != nil {
		e.degrade(path, f, CauseServerUnreachable, err.Error())
		return false
	}
	ask := w.SessionKey
	if f.Plan == string(PlanPublic) {
		ask = w.PublicSessionKey
	}
	ans, err := ask(login.AccessToken, f.Repo, f.Nonce, e.Engine)
	var ref *licenseapi.Refusal
	if errors.As(err, &ref) && ref.Status == http.StatusUnauthorized {
		l, ok := e.refresh(login, timeout)
		if !ok {
			e.degrade(path, f, CauseLoginExpired, err.Error())
			return false
		}
		ans, err = ask(l.AccessToken, f.Repo, f.Nonce, e.Engine)
	}
	switch {
	case err == nil:
	case licenseapi.IsUnreachable(err), errors.As(err, &ref) && ref.Status >= 500:
		if e.fromCache(path, f, origin, f.UserID) {
			return true
		}
		e.degrade(path, f, CauseServerUnreachable, err.Error())
		return false
	case errors.As(err, &ref) && ref.Reason != "":
		e.degradeRefused(path, f, ref)
		return false
	default:
		e.degrade(path, f, CauseServerUnreachable, err.Error())
		return false
	}
	if !e.land(path, f, []byte(ans.Key), landedBy) {
		return false
	}
	_ = st.WriteKey(origin, CachedKey{Key: ans.Key, FetchedAt: e.Now(), UserID: f.UserID, Nonce: f.Nonce, Identity: *f.Identity})
	return true
}

// fromCache lands the repo's cached key, bound to the nonce it was
// fetched with, when it is still valid.
func (e Env) fromCache(path string, f *File, origin string, user int64) bool {
	c, _, ok := e.store().Key(origin, user, e.Roots, e.Now())
	if !ok {
		return false
	}
	f.UserID, f.Identity = c.UserID, &c.Identity
	return e.write(path, f, func(cur *File) bool {
		now := e.Now()
		cur.State, cur.Key, cur.KeyNonce, cur.LandedAt, cur.Path = StateLanded, c.Key, c.Nonce, &now, PathCache
		cur.Cause, cur.CauseDetail, cur.Link = "", "", ""
		return true
	})
}

// refresh exchanges the stored refresh token through the Worker and
// stores GitHub's answer.
func (e Env) refresh(l Login, timeout time.Duration) (Login, bool) {
	if l.RefreshToken == "" {
		return Login{}, false
	}
	w, err := e.Worker(timeout)
	if err != nil {
		return Login{}, false
	}
	t, err := w.Refresh(l.RefreshToken)
	if err != nil {
		e.logf("refresh: %v", err)
		return Login{}, false
	}
	nl := Login{AccessToken: t.AccessToken, ExpiresIn: t.ExpiresIn, RefreshToken: t.RefreshToken,
		RefreshTokenExpiresIn: t.RefreshTokenExpiresIn, ObtainedAt: e.Now(), UserID: l.UserID, UserLogin: l.UserLogin}
	if err := e.store().WriteLogin(nl); err != nil {
		return Login{}, false
	}
	return nl, true
}
