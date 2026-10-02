package license

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/licenseapi"
)

const testHead = "0123456789abcdef0123456789abcdef01234567"

// fakeGH is GitHub for one session: the dispatch it records makes a check
// run appear after delay polls, carrying answer(nonce) as the key (or a
// refusal when refuse is set).
type fakeGH struct {
	mu          sync.Mutex
	t           *testing.T
	calls       []string
	userType    string
	private     bool
	dispatchErr error
	noApp       bool
	delay       int
	polls       int
	nonce       string
	refuse      string
	summary     string
	keyEdits    func(nonce string) map[string]any
	unreachable bool
	// impostor puts first a run carrying the session's nonce and a key
	// minted for another user, as one dispatched by a reader who copied
	// the nonce; impostorOnly leaves the session's own run out.
	impostor, impostorOnly bool
}

func (g *fakeGH) record(c string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, c)
}

func (g *fakeGH) count(prefix string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (g *fakeGH) User() (githubapi.Account, error) {
	g.record("user")
	if g.unreachable {
		return githubapi.Account{}, os.ErrDeadlineExceeded
	}
	typ := g.userType
	if typ == "" {
		typ = "User"
	}
	return githubapi.Account{ID: 7, Login: "acme-user", Type: typ}, nil
}

func (g *fakeGH) RepoInfo() (githubapi.RepoInfo, error) {
	g.record("repo")
	r := githubapi.RepoInfo{ID: 11, Name: "member", FullName: "acme/member", Private: g.private, DefaultBranch: "main"}
	r.Owner = githubapi.Account{ID: 3, Login: "acme", Type: "User"}
	return r, nil
}

func (g *fakeGH) BranchHead(string) (string, error) { g.record("head"); return testHead, nil }

func (g *fakeGH) RepositoryDispatch(eventType string, payload any) error {
	p := payload.(map[string]string)
	g.record("dispatch " + eventType + " " + p["head"])
	if g.dispatchErr != nil {
		return g.dispatchErr
	}
	g.mu.Lock()
	g.nonce = p["nonce"]
	g.mu.Unlock()
	return nil
}

func (g *fakeGH) CheckRuns(sha, name string) ([]githubapi.CheckRun, error) {
	g.record("check-runs " + sha)
	g.mu.Lock()
	g.polls++
	polls, nonce := g.polls, g.nonce
	g.mu.Unlock()
	if g.noApp || nonce == "" || polls <= g.delay {
		return nil, nil
	}
	r := githubapi.CheckRun{ID: 1, Name: CheckRunName, ExternalID: nonce, Status: "completed", Conclusion: "neutral"}
	if g.refuse != "" {
		r.Output.Title, r.Output.Summary = CheckRunName+" refused", g.refuse+": the reason"
		if g.summary != "" {
			r.Output.Summary = g.summary
		}
		return []githubapi.CheckRun{r}, nil
	}
	edits := map[string]any{"nonce": nonce}
	if g.keyEdits != nil {
		edits = g.keyEdits(nonce)
	}
	r.Output.Title, r.Output.Text = CheckRunName, string(mint(g.t, edits))
	other := r
	other.ExternalID = "someone-elses-nonce"
	other.Output.Text = "not this one"
	runs := []githubapi.CheckRun{other, r}
	if g.impostor || g.impostorOnly {
		fake := r
		fake.ID = 2
		edits["user_id"] = 8
		fake.Output.Text = string(mint(g.t, edits))
		runs = []githubapi.CheckRun{other, fake, r}
		if g.impostorOnly {
			runs = runs[:2]
		}
	}
	return runs, nil
}

// fakeWorker answers the desktop and Actions routes.
type fakeWorker struct {
	t         *testing.T
	calls     []string
	down      bool
	refuse    *licenseapi.Refusal
	expire    int // 401s to answer before a key
	refreshOK bool
	edits     func(nonce string) map[string]any
}

func (w *fakeWorker) Refresh(rt string) (licenseapi.TokenAnswer, error) {
	w.calls = append(w.calls, "refresh "+rt)
	if !w.refreshOK {
		return licenseapi.TokenAnswer{}, &licenseapi.Refusal{Status: 503, Reason: "refresh-not-configured"}
	}
	return licenseapi.TokenAnswer{AccessToken: "ghu_new", RefreshToken: "ghr_new", ExpiresIn: 28800}, nil
}

func (w *fakeWorker) key(path, tok, nonce string) (licenseapi.KeyAnswer, error) {
	w.calls = append(w.calls, path+" "+tok)
	if w.down {
		return licenseapi.KeyAnswer{}, os.ErrDeadlineExceeded
	}
	if w.expire > 0 {
		w.expire--
		return licenseapi.KeyAnswer{}, &licenseapi.Refusal{Status: 401, Reason: "token-invalid"}
	}
	if w.refuse != nil {
		return licenseapi.KeyAnswer{}, w.refuse
	}
	edits := map[string]any{"nonce": nonce}
	if w.edits != nil {
		edits = w.edits(nonce)
	}
	return licenseapi.KeyAnswer{Key: string(mint(w.t, edits)), Plan: "public", State: "ok"}, nil
}

func (w *fakeWorker) SessionKey(tok, repo, nonce, engine string) (licenseapi.KeyAnswer, error) {
	return w.key("session-key", tok, nonce)
}
func (w *fakeWorker) PublicSessionKey(tok, repo, nonce, engine string) (licenseapi.KeyAnswer, error) {
	return w.key("public-session-key", tok, nonce)
}
func (w *fakeWorker) ActionsKey(tok, engine string) (licenseapi.KeyAnswer, error) {
	return w.key("actions-key", tok, "")
}

// rig is one session's world: a fake clock, GitHub, the Worker and the
// detached starts SessionStart asked for.
type rig struct {
	t      *testing.T
	now    time.Time
	gh     *fakeGH
	w      *fakeWorker
	env    Env
	starts []string
	// budgets is the timeout of each Worker client the session built.
	budgets []time.Duration
	origin  string
	vars    map[string]string
	log     bytes.Buffer
}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, now: testNow, gh: &fakeGH{t: t}, w: &fakeWorker{t: t}, origin: "https://github.com/acme/member.git", vars: map[string]string{}}
	r.env = Env{
		CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Roots: testRoots(), Engine: "60930.1.0",
		Now: func() time.Time { return r.now }, Sleep: func(d time.Duration) { r.now = r.now.Add(d) },
		Getenv: func(k string) string { return r.vars[k] },
		GitHub: func(repo, token string, timeout time.Duration) GitHub {
			r.gh.record("client " + repo + " token=" + token)
			return r.gh
		},
		Worker: func(timeout time.Duration) (Worker, error) {
			r.budgets = append(r.budgets, timeout)
			return r.w, nil
		},
		Origin: func(string) (string, error) { return r.origin, nil },
		Plan:   func(string) string { return "public" },
		Start: func(id, nonce, dir string) error {
			r.starts = append(r.starts, id+" "+nonce)
			return nil
		},
		Log: &r.log,
	}
	return r
}

func (r *rig) file() *File {
	r.t.Helper()
	p, _ := StatePath(r.env.CacheRoot, "s1")
	f, err := ReadState(p)
	if err != nil || f == nil {
		r.t.Fatalf("state file: %v %v", f, err)
	}
	return f
}

// start runs SessionStart and then the background request it asked for.
func (r *rig) start() Status {
	r.t.Helper()
	s := r.env.SessionStart("/repo", "s1")
	if len(r.starts) == 0 {
		return s
	}
	nonce := strings.Fields(r.starts[len(r.starts)-1])[1]
	if err := r.env.RunRequest("s1", nonce, "/repo"); err != nil {
		r.t.Fatal(err)
	}
	return s
}

func TestWebKeyLands(t *testing.T) {
	r := newRig(t)
	r.gh.delay = 3
	s := r.env.SessionStart("/repo", "s1")
	if !s.Verdict.Pending || !strings.Contains(s.Line, "license pending") || !s.Gates.On(SurfaceWorkChecks) {
		t.Fatalf("SessionStart %+v", s)
	}
	if len(r.starts) != 1 {
		t.Fatalf("starts %v", r.starts)
	}
	f := r.file()
	if f.State != StatePending || f.Repo != "acme/member" || f.Mode != PathWeb || f.Plan != "public" || len(f.Nonce) < 16 {
		t.Fatalf("pending file %+v", f)
	}
	if err := r.env.RunRequest("s1", f.Nonce, "/repo"); err != nil {
		t.Fatal(err)
	}
	f = r.file()
	if f.State != StateLanded || f.Path != PathWeb || f.UserID != 7 || f.Identity == nil || f.Identity.ID != 11 || f.KeyNonce != f.Nonce {
		t.Fatalf("landed file %+v", f)
	}
	if r.gh.count("dispatch claudinite-key-public "+testHead) != 1 {
		t.Errorf("calls %v", r.gh.calls)
	}
	h := r.env.Hook("/repo", "s1")
	if h.Verdict.Key == nil || !h.Gates.On(SurfaceWorkChecks) || h.Notice != "" {
		t.Fatalf("hook %+v", h)
	}
	if !strings.Contains(r.log.String(), "[cn] license request-web ok") {
		t.Errorf("log %s", r.log.String())
	}
}

func TestThePaidPlanDispatchesThePaidEvent(t *testing.T) {
	r := newRig(t)
	r.env.Plan = func(string) string { return "" }
	r.start()
	if r.gh.count("dispatch claudinite-key "+testHead) != 1 {
		t.Errorf("calls %v", r.gh.calls)
	}
}

func TestSessionStartAddsLittleTime(t *testing.T) {
	r := newRig(t)
	r.env.Now = time.Now
	start := time.Now()
	r.env.SessionStart("/repo", "s1")
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("SessionStart took %v with a cold state", d)
	}
}

func TestARefusedDispatchDegradesAtOnce(t *testing.T) {
	r := newRig(t)
	r.gh.dispatchErr = &githubapi.HTTPError{Status: 403, StatusText: "403 Forbidden"}
	r.start()
	f := r.file()
	if f.State != StateDegraded || f.Cause != CauseNoPushAccess {
		t.Fatalf("%+v", f)
	}
	if r.gh.count("check-runs") != 0 {
		t.Errorf("check runs read after a refused dispatch: %v", r.gh.calls)
	}
	if r.now.Sub(testNow) > time.Second {
		t.Errorf("waited %v", r.now.Sub(testNow))
	}
	h := r.env.Hook("/repo", "s1")
	if h.Gates.On(SurfaceWorkChecks) || !strings.Contains(h.Notice, "push access") {
		t.Fatalf("hook %+v", h)
	}
}

func TestNoAppDegradesAtTheCutAndPollsUntilTheTailEnds(t *testing.T) {
	r := newRig(t)
	r.gh.noApp = true
	r.start()
	f := r.file()
	if f.State != StateDegraded || f.Cause != CauseAppNotInstalled || f.Link != InstallURL {
		t.Fatalf("%+v", f)
	}
	if got := r.now.Sub(testNow); got < Cut+DefaultTail || got > Cut+DefaultTail+time.Second {
		t.Errorf("the request stopped after %v", got)
	}
	if n := r.gh.count("check-runs"); n < 250 || n > 262 {
		t.Errorf("%d check-run reads in 130 s at 500 ms", n)
	}
	if !strings.Contains(r.log.String(), "[cn] license cut timeout") || !strings.Contains(r.log.String(), "[cn] license request-web timeout") {
		t.Errorf("log %s", r.log.String())
	}
}

func TestAShorterTail(t *testing.T) {
	t.Setenv("CLAUDINITE_LICENSE_TAIL_MS", "2000")
	r := newRig(t)
	r.gh.noApp = true
	r.start()
	if got := r.now.Sub(testNow); got < Cut+2*time.Second || got > Cut+3*time.Second {
		t.Errorf("the request stopped after %v", got)
	}
}

func TestALateKeyTurnsTheFeaturesBackOn(t *testing.T) {
	r := newRig(t)
	r.gh.delay = 30 // 15 s at 500 ms
	r.env.SessionStart("/repo", "s1")
	f := r.file()
	// The detached request is slow to write; a hook at +11 s sees the cut.
	r.gh.noApp = true
	done := make(chan struct{})
	var mu sync.Mutex
	r.env.Sleep = func(d time.Duration) {
		mu.Lock()
		r.now = r.now.Add(d)
		mu.Unlock()
		if r.now.Sub(testNow) == 11*time.Second {
			h := r.env.Hook("/repo", "s1")
			if h.Gates.On(SurfaceWorkChecks) || !strings.Contains(h.Notice, InstallURL) {
				t.Errorf("hook at +11 s: %+v", h)
			}
			r.gh.noApp = false
		}
	}
	go func() { _ = r.env.RunRequest("s1", f.Nonce, "/repo"); close(done) }()
	<-done
	f = r.file()
	if f.State != StateLanded {
		t.Fatalf("%+v", f)
	}
	if h := r.env.Hook("/repo", "s1"); !h.Gates.On(SurfaceWorkChecks) {
		t.Errorf("after the late key: %+v", h)
	}
}

func TestARefusedCheckRunDegradesNamingTheReason(t *testing.T) {
	r := newRig(t)
	r.gh.refuse = "no-plan"
	r.start()
	f := r.file()
	if f.State != StateDegraded || f.Cause != "no-plan" || f.Link != InstallURL || f.CauseDetail != "the reason" {
		t.Fatalf("%+v", f)
	}
	if h := r.env.Hook("/repo", "s1"); !strings.Contains(h.Notice, "no plan") {
		t.Errorf("%+v", h)
	}
}

// The public Worker's private-repo refusal, with its reason prefix and
// without it (as it was written before the prefix), is refused-private,
// with the link where an owner picks a plan.
func TestThePublicWorkersRefusalIsAPrivateRepoCause(t *testing.T) {
	for _, summary := range []string{
		"refused-private: this repo is private; the Public plan covers public repos only",
		"this repo is private; the Public plan covers public repos only",
	} {
		r := newRig(t)
		r.gh.refuse, r.gh.summary = "x", summary
		r.start()
		f := r.file()
		if f.State != StateDegraded || f.Cause != CauseRefusedPrivate || f.Link != InstallURL || f.CauseDetail != "this repo is private; the Public plan covers public repos only" {
			t.Fatalf("%q: %+v", summary, f)
		}
		if h := r.env.Hook("/repo", "s1"); !strings.Contains(h.Notice, "private") || !strings.Contains(h.Notice, InstallURL) {
			t.Errorf("%q: %+v", summary, h)
		}
	}
}

func TestBindFailuresDegrade(t *testing.T) {
	for name, c := range map[string]struct {
		private bool
		edits   func(string) map[string]any
		want    Cause
	}{
		"wrong nonce":           {false, func(string) map[string]any { return map[string]any{"nonce": "nonce-other-000000"} }, CauseBindNonce},
		"public key on private": {true, nil, CauseBindPlan},
		"unverified on private": {true, func(n string) map[string]any { return map[string]any{"nonce": n, "state": "unverified"} }, ""},
		"another user":          {false, func(n string) map[string]any { return map[string]any{"nonce": n, "user_id": 8} }, CauseBindUser},
		"expired before landing": {false, func(n string) map[string]any {
			return map[string]any{"nonce": n, "exp": testNow.Add(-time.Hour).Unix()}
		}, CauseKeyRefused},
	} {
		r := newRig(t)
		r.gh.private, r.gh.keyEdits = c.private, c.edits
		r.start()
		f := r.file()
		if c.want == "" {
			if f.State != StateLanded {
				t.Errorf("%s: %+v", name, f)
			}
			continue
		}
		if f.State != StateDegraded || f.Cause != c.want {
			t.Errorf("%s: %s %s, want %s", name, f.State, f.Cause, c.want)
		}
	}
}

func TestAHookPollsOnceWhileTheKeyIsPending(t *testing.T) {
	r := newRig(t)
	r.env.SessionStart("/repo", "s1")
	f := r.file()
	gh := r.gh
	gh.delay = 1000
	// The background request dispatched and then died.
	path, _ := StatePath(r.env.CacheRoot, "s1")
	f.UserID, f.Identity, f.Head, f.Dispatched = 7, &RepoIdentity{ID: 11, OwnerID: 3, OwnerLogin: "acme"}, testHead, true
	_ = UpdateState(path, func(*File) (*File, bool) { return f, true })
	gh.nonce = f.Nonce
	r.now = r.now.Add(2 * time.Second)
	h := r.env.Hook("/repo", "s1")
	if h.Verdict.Key != nil || !h.Verdict.Pending || gh.count("check-runs") != 1 {
		t.Fatalf("hook %+v, calls %v", h, gh.calls)
	}
	gh.delay = 0
	h = r.env.Hook("/repo", "s1")
	if h.Verdict.Key == nil || r.file().Path != PathHookPoll || len(h.Crumbs) == 0 {
		t.Fatalf("hook %+v file %+v", h, r.file())
	}
	before := gh.count("check-runs")
	r.env.Hook("/repo", "s1")
	if gh.count("check-runs") != before {
		t.Error("a hook polled with the key landed")
	}
}

func TestTheCutIsWrittenByAHookAndNoticedOnce(t *testing.T) {
	r := newRig(t)
	r.env.SessionStart("/repo", "s1")
	r.now = r.now.Add(11 * time.Second)
	h := r.env.Hook("/repo", "s1")
	if h.Gates.On(SurfaceWorkChecks) || h.Notice == "" || !strings.Contains(h.Notice, "GitHub did not answer") {
		t.Fatalf("hook %+v", h)
	}
	if f := r.file(); f.State != StateDegraded || f.Cause != CauseGitHubUnreachable {
		t.Fatalf("%+v", f)
	}
	if h := r.env.Hook("/repo", "s1"); h.Notice != "" {
		t.Errorf("the notice repeated: %q", h.Notice)
	}
}

func TestAResumeReusesTheKey(t *testing.T) {
	r := newRig(t)
	r.start()
	before, _ := os.ReadFile(filepath.Join(SessionsDir(r.env.CacheRoot), "s1.json"))
	r.now = r.now.Add(time.Hour)
	s := r.env.SessionStart("/repo", "s1")
	if len(r.starts) != 1 || s.Verdict.Key == nil || !strings.Contains(s.Line, "license ok: public, ok") {
		t.Fatalf("starts %v status %+v", r.starts, s)
	}
	after, _ := os.ReadFile(filepath.Join(SessionsDir(r.env.CacheRoot), "s1.json"))
	if !bytes.Equal(before, after) {
		t.Errorf("a resume rewrote the state file")
	}
}

func TestADayOldKeyRenewsAndStaysInUse(t *testing.T) {
	r := newRig(t)
	r.start()
	old := r.file()
	r.now = r.now.Add(RenewAge + time.Minute)
	h := r.env.Hook("/repo", "s1")
	if h.Verdict.Key == nil || !h.Gates.On(SurfaceWorkChecks) || len(r.starts) != 2 {
		t.Fatalf("hook %+v starts %v", h, r.starts)
	}
	mid := r.file()
	if mid.Key != old.Key || mid.Nonce == old.Nonce {
		t.Fatalf("renewal file %+v", mid)
	}
	if h := r.env.Hook("/repo", "s1"); len(r.starts) != 2 || h.Verdict.Key == nil {
		t.Errorf("a second hook started another renewal: %v", r.starts)
	}
	if err := r.env.RunRequest("s1", mid.Nonce, "/repo"); err != nil {
		t.Fatal(err)
	}
	if f := r.file(); f.Key == old.Key || f.KeyNonce != mid.Nonce || f.State != StateLanded {
		t.Fatalf("renewed file %+v", f)
	}
}

func TestAFailedRenewalKeepsTheOldKey(t *testing.T) {
	r := newRig(t)
	r.start()
	old := r.file()
	r.now = r.now.Add(RenewAge + time.Minute)
	r.env.Hook("/repo", "s1")
	r.gh.dispatchErr = &githubapi.HTTPError{Status: 403}
	_ = r.env.RunRequest("s1", r.file().Nonce, "/repo")
	if f := r.file(); f.Key != old.Key || f.State != StateLanded {
		t.Fatalf("%+v", f)
	}
}

// A renewal that keeps failing backs off, 1, 2, 4 ... 32 minutes after
// each attempt's tail, and stops after MaxRenewals in one session.
func TestAFailingRenewalBacksOffAndIsCapped(t *testing.T) {
	r := newRig(t)
	r.start()
	r.gh.dispatchErr = &githubapi.HTTPError{Status: 403}
	r.now = r.now.Add(RenewAge + time.Minute)
	fail := func() {
		t.Helper()
		_ = r.env.RunRequest("s1", r.file().Nonce, "/repo")
	}
	r.env.Hook("/repo", "s1")
	if len(r.starts) != 2 {
		t.Fatalf("no first renewal: %v", r.starts)
	}
	fail()
	for attempt := 1; attempt < MaxRenewals; attempt++ {
		wait := time.Minute << (attempt - 1)
		if wait > 32*time.Minute {
			wait = 32 * time.Minute
		}
		ends := r.file().RequestedAt.Add(Cut + Tail())
		r.now = ends.Add(wait - time.Second)
		r.env.Hook("/repo", "s1")
		if len(r.starts) != attempt+1 {
			t.Fatalf("attempt %d: renewed %v early (backoff %v)", attempt, r.starts, wait)
		}
		r.now = ends.Add(wait)
		r.env.Hook("/repo", "s1")
		if len(r.starts) != attempt+2 {
			t.Fatalf("attempt %d: no renewal after %v: %v", attempt, wait, r.starts)
		}
		fail()
	}
	r.now = r.now.Add(24 * time.Hour)
	if h := r.env.Hook("/repo", "s1"); len(r.starts) != MaxRenewals+1 || h.Verdict.Key == nil {
		t.Errorf("past the cap: %d starts, key %v", len(r.starts), h.Verdict.Key != nil)
	}
	// A renewal that lands resets the count.
	r.gh.dispatchErr = nil
	f := r.file()
	f.RenewAttempts = 1
	_ = UpdateState(mustPath(t, r), func(*File) (*File, bool) { return f, true })
	r.now = f.RequestedAt.Add(Cut + Tail() + time.Minute)
	r.env.Hook("/repo", "s1")
	if err := r.env.RunRequest("s1", r.file().Nonce, "/repo"); err != nil {
		t.Fatal(err)
	}
	if got := r.file(); got.State != StateLanded || got.RenewAttempts != 0 {
		t.Errorf("after a landed renewal: %+v", got)
	}
}

func mustPath(t *testing.T, r *rig) string {
	t.Helper()
	p, err := StatePath(r.env.CacheRoot, "s1")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A check run carrying the session's nonce with another user's key is
// someone else's request: it is passed over, not taken as a bind failure.
func TestThePollPassesOverAnotherUsersRunWithTheSameNonce(t *testing.T) {
	r := newRig(t)
	r.gh.impostor = true
	r.start()
	if f := r.file(); f.State != StateLanded || f.Cause != "" {
		t.Fatalf("with both runs: %+v", f)
	}

	// With only the other user's run, the cut names it.
	r = newRig(t)
	r.gh.impostorOnly = true
	r.start()
	if f := r.file(); f.State != StateDegraded || f.Cause != CauseBindUser {
		t.Fatalf("with only the other user's run: %+v", f)
	}
}

func TestAMalformedStateFileDegrades(t *testing.T) {
	r := newRig(t)
	path, _ := StatePath(r.env.CacheRoot, "s1")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte("{"), 0o600)
	h := r.env.Hook("/repo", "s1")
	if h.Gates.On(SurfaceWorkChecks) || h.Verdict.Cause != CauseStateFile {
		t.Fatalf("%+v", h)
	}
}

func TestATamperedKeyDegradesAtTheNextHook(t *testing.T) {
	r := newRig(t)
	r.start()
	path, _ := StatePath(r.env.CacheRoot, "s1")
	_ = UpdateState(path, func(f *File) (*File, bool) {
		f.Key = strings.Replace(f.Key, `"signature":"`, `"signature":"A`, 1)
		return f, true
	})
	if h := r.env.Hook("/repo", "s1"); h.Verdict.Key != nil || h.Gates.On(SurfaceWorkChecks) || h.Verdict.Cause != CauseKeyRefused {
		t.Fatalf("%+v", h)
	}
}

func TestAnExpiredKeyDegrades(t *testing.T) {
	r := newRig(t)
	r.start()
	r.now = r.now.Add(8 * 24 * time.Hour)
	h := r.env.Hook("/repo", "s1")
	if h.Verdict.Key != nil || h.Verdict.Detail != string(ReasonKeyExpired) {
		t.Fatalf("%+v", h)
	}
}

func TestNoRequestInActionsOrWithoutAGitHubOrigin(t *testing.T) {
	r := newRig(t)
	r.vars["GITHUB_ACTIONS"] = "true"
	s := r.env.SessionStart("/repo", "s1")
	if len(r.starts) != 0 || s.Verdict.Cause != CauseActions {
		t.Errorf("actions: %v %+v", r.starts, s)
	}
	r = newRig(t)
	r.origin = "/tmp/origin.git"
	s = r.env.SessionStart("/repo", "s1")
	if len(r.starts) != 0 || s.Verdict.Cause != CauseNoGitHubRemote || s.Gates.On(SurfaceWorkChecks) {
		t.Errorf("local origin: %v %+v", r.starts, s)
	}
}

func TestABadSessionIDIsRefused(t *testing.T) {
	r := newRig(t)
	if s := r.env.SessionStart("/repo", "../x"); s.Verdict.Cause != CauseStateFile || len(r.starts) != 0 {
		t.Errorf("%+v", s)
	}
}

func desktopRig(t *testing.T) *rig {
	r := newRig(t)
	if err := r.env.store().WriteLogin(Login{AccessToken: "ghu_old", RefreshToken: "ghr_old", ExpiresIn: 28800, ObtainedAt: testNow, UserID: 7}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDesktopKeyLandsAndIsCached(t *testing.T) {
	r := desktopRig(t)
	r.start()
	f := r.file()
	if f.Mode != PathDesktop || f.State != StateLanded || f.Path != PathDesktop {
		t.Fatalf("%+v", f)
	}
	if len(r.w.calls) != 1 || r.w.calls[0] != "public-session-key ghu_old" || r.gh.count("client acme/member token=ghu_old") == 0 {
		t.Errorf("worker %v github %v", r.w.calls, r.gh.calls)
	}
	st, _ := os.Stat(r.env.store().KeyPath(r.origin))
	if st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf("key cache %v", st)
	}
	if st, _ := os.Stat(r.env.store().LoginPath()); st.Mode().Perm() != 0o600 {
		t.Errorf("login mode %v", st.Mode())
	}
}

// A desktop hook's read for a pending key is bounded like a web hook's.
func TestADesktopHookPollKeepsToTheHookBudget(t *testing.T) {
	r := desktopRig(t)
	r.env.SessionStart("/repo", "s1")
	f := r.file()
	f.UserID, f.Identity = 7, &RepoIdentity{ID: 11, OwnerID: 3, OwnerLogin: "acme"}
	_ = UpdateState(mustPath(t, r), func(*File) (*File, bool) { return f, true })
	r.now = r.now.Add(2 * time.Second)
	h := r.env.Hook("/repo", "s1")
	if h.Verdict.Key == nil || r.file().Path != PathHookPoll {
		t.Fatalf("hook %+v file %+v", h, r.file())
	}
	if len(r.budgets) != 1 || r.budgets[0] != HookPollBudget {
		t.Errorf("worker budgets %v, want [%v]", r.budgets, HookPollBudget)
	}
}

func TestDesktopFallsBackToTheCacheThenDegrades(t *testing.T) {
	r := desktopRig(t)
	r.start()
	r.w.down = true
	r.env.SessionStart("/repo", "s2")
	nonce := strings.Fields(r.starts[len(r.starts)-1])[1]
	_ = r.env.RunRequest("s2", nonce, "/repo")
	p, _ := StatePath(r.env.CacheRoot, "s2")
	f, _ := ReadState(p)
	if f.State != StateLanded || f.Path != PathCache {
		t.Fatalf("cache: %+v", f)
	}
	if h := r.env.Hook("/repo", "s2"); h.Verdict.Key == nil {
		t.Fatalf("the cached key did not apply: %+v", h)
	}
	r.now = r.now.Add(8 * 24 * time.Hour)
	r.env.SessionStart("/repo", "s3")
	nonce = strings.Fields(r.starts[len(r.starts)-1])[1]
	_ = r.env.RunRequest("s3", nonce, "/repo")
	p, _ = StatePath(r.env.CacheRoot, "s3")
	f, _ = ReadState(p)
	if f.State != StateDegraded || f.Cause != CauseServerUnreachable {
		t.Fatalf("aged cache: %+v", f)
	}
}

func TestDesktopRefreshesOnce(t *testing.T) {
	r := desktopRig(t)
	r.w.expire, r.w.refreshOK = 1, true
	r.start()
	if f := r.file(); f.State != StateLanded {
		t.Fatalf("%+v %v", f, r.w.calls)
	}
	if got := strings.Join(r.w.calls, ","); got != "public-session-key ghu_old,refresh ghr_old,public-session-key ghu_new" {
		t.Errorf("calls %s", got)
	}
	if l, _ := r.env.store().ReadLogin(); l.AccessToken != "ghu_new" || l.UserID != 7 {
		t.Errorf("stored login %+v", l)
	}
}

func TestDesktopWithoutARefreshSaysLogIn(t *testing.T) {
	r := desktopRig(t)
	r.w.expire = 1
	r.start()
	f := r.file()
	if f.State != StateDegraded || f.Cause != CauseLoginExpired {
		t.Fatalf("%+v", f)
	}
	if h := r.env.Hook("/repo", "s1"); !strings.Contains(h.Notice, "cn login") {
		t.Errorf("%q", h.Notice)
	}
}

func TestDesktopRefusalNamesTheReason(t *testing.T) {
	r := desktopRig(t)
	r.w.refuse = &licenseapi.Refusal{Status: 403, Reason: "refused-private"}
	r.start()
	if f := r.file(); f.Cause != "refused-private" {
		t.Fatalf("%+v", f)
	}
}

func oidcServer(t *testing.T) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("audience") != Audience {
			t.Errorf("audience %q", r.URL.Query().Get("audience"))
		}
		_, _ = w.Write([]byte(`{"value": "h.eyJyZXBvc2l0b3J5X3Zpc2liaWxpdHkiOiJwdWJsaWMifQ.s"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func actionsEnv(t *testing.T, srv *httptest.Server, private bool) map[string]string {
	ev := filepath.Join(t.TempDir(), "event.json")
	raw, _ := json.Marshal(map[string]any{"repository": map[string]any{"id": 11, "private": private, "owner": map[string]any{"id": 3, "login": "acme", "type": "User"}}})
	_ = os.WriteFile(ev, raw, 0o600)
	return map[string]string{"ACTIONS_ID_TOKEN_REQUEST_URL": srv.URL + "/t?x=1", "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req",
		"GITHUB_REPOSITORY_ID": "11", "GITHUB_REPOSITORY_OWNER_ID": "3", "GITHUB_EVENT_PATH": ev}
}

func TestActionsKey(t *testing.T) {
	srv := oidcServer(t)
	actions := func(n string) map[string]any { return map[string]any{"typ": "actions", "user_id": nil, "nonce": nil} }
	now := func() time.Time { return testNow }
	env := actionsEnv(t, srv, false)
	w := &fakeWorker{t: t, edits: actions}
	res := RequestActions(w, srv.Client(), func(k string) string { return env[k] }, testRoots(), now, "1")
	if res.Key == nil || res.Cause != "" || w.calls[0] != "actions-key h.eyJyZXBvc2l0b3J5X3Zpc2liaWxpdHkiOiJwdWJsaWMifQ.s" {
		t.Fatalf("%+v %v", res, w.calls)
	}
	if _, err := VerifyKey([]byte(res.Wire), testRoots(), testNow); err != nil {
		t.Errorf("the result carries the key's wire form, the grant's bearer: %v", err)
	}
	if res := RequestActions(w, srv.Client(), func(string) string { return "" }, testRoots(), now, "1"); res.Cause != CauseNoOIDC {
		t.Errorf("no OIDC: %+v", res)
	}
	w.refuse = &licenseapi.Refusal{Status: 403, Reason: "app-not-installed"}
	if res := RequestActions(w, srv.Client(), func(k string) string { return env[k] }, testRoots(), now, "1"); res.Cause != CauseAppNotInstalled || res.Link != InstallURL {
		t.Errorf("no App: %+v", res)
	}
	w.refuse = &licenseapi.Refusal{Status: 503, Reason: "server-error"}
	if res := RequestActions(w, srv.Client(), func(k string) string { return env[k] }, testRoots(), now, "1"); res.Cause != CauseServerUnreachable {
		t.Errorf("503: %+v", res)
	}
	w.refuse, w.down = nil, true
	if res := RequestActions(w, srv.Client(), func(k string) string { return env[k] }, testRoots(), now, "1"); res.Cause != CauseServerUnreachable {
		t.Errorf("down: %+v", res)
	}
	w.down = false
	priv := actionsEnv(t, srv, true)
	if res := RequestActions(w, srv.Client(), func(k string) string { return priv[k] }, testRoots(), now, "1"); res.Cause != CauseBindPlan {
		t.Errorf("a public key on a private repo: %+v", res)
	}
	noEvent := actionsEnv(t, srv, true)
	noEvent["GITHUB_EVENT_PATH"] = ""
	if res := RequestActions(w, srv.Client(), func(k string) string { return noEvent[k] }, testRoots(), now, "1"); res.Key == nil {
		t.Errorf("visibility from the token's claim: %+v", res)
	}
}

func TestAHookWithNoStateFileGatesWithoutANotice(t *testing.T) {
	r := newRig(t)
	s := r.env.Hook(t.TempDir(), "never-started")
	if s.Notice != "" || s.Gates.On(SurfaceWorkChecks) || s.Verdict.Cause != CauseStateFile {
		t.Errorf("%+v", s)
	}
}

func TestDesktopFallsBackToTheCacheOnAServerError(t *testing.T) {
	r := desktopRig(t)
	r.start()
	r.w.refuse = &licenseapi.Refusal{Status: 503, Reason: "server-error"}
	r.env.SessionStart("/repo", "s2")
	nonce := strings.Fields(r.starts[len(r.starts)-1])[1]
	_ = r.env.RunRequest("s2", nonce, "/repo")
	p, _ := StatePath(r.env.CacheRoot, "s2")
	if f, _ := ReadState(p); f.State != StateLanded || f.Path != PathCache {
		t.Fatalf("%+v", f)
	}
}
