package license

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
)

// The session state file's states and the paths that land a key.
const (
	StatePending  = "pending"
	StateLanded   = "landed"
	StateDegraded = "degraded"

	PathWeb      = "web"
	PathDesktop  = "desktop"
	PathCache    = "cache"
	PathHookPoll = "hook-poll"
)

const (
	// Cut is how long a session runs with every feature on before a
	// request with no key degrades.
	Cut = 10 * time.Second
	// DefaultTail is how long after the cut a late key is still looked for.
	DefaultTail = 120 * time.Second
	// RenewAge is the age of a landed key at which a hook renews it.
	RenewAge = 24 * time.Hour
	// MaxRenewals caps the renewals one session starts; the key they
	// would replace stays in use until it expires.
	MaxRenewals = 10
	// renewBackoffCap bounds the wait between failed renewals.
	renewBackoffCap = 32 * time.Minute
)

// renewDue reports whether a renewal may start now: the first at once, a
// later one when the previous attempt's tail is over plus 1, 2, 4 ... 32
// minutes, and none past MaxRenewals.
func renewDue(f *File, now time.Time) bool {
	switch {
	case f.RenewAttempts == 0:
		return true
	case f.RenewAttempts >= MaxRenewals:
		return false
	}
	wait := renewBackoffCap
	if f.RenewAttempts <= 6 {
		wait = min(time.Minute<<(f.RenewAttempts-1), renewBackoffCap)
	}
	return !now.Before(f.RequestedAt.Add(Cut + Tail() + wait))
}

// Tail is DefaultTail, or CLAUDINITE_LICENSE_TAIL_MS milliseconds (the
// rehearsal shortens it).
func Tail() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("CLAUDINITE_LICENSE_TAIL_MS")); err == nil && ms >= 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return DefaultTail
}

// File is one session's license state: the request in flight or last made,
// the key it landed, or why there is none. Every hook reads it; the
// background request and a hook's own poll write it.
type File struct {
	V         int    `json:"v"`
	SessionID string `json:"session_id"`
	// Repo is owner/name, read from the checkout's origin.
	Repo string `json:"repo"`
	// Mode is the request path: web or desktop.
	Mode string `json:"mode"`
	// Dir is the checkout the session runs in.
	Dir string `json:"dir,omitempty"`
	// Plan is the settings file's license.plan, which picks the endpoint.
	Plan        string        `json:"plan,omitempty"`
	Nonce       string        `json:"nonce"`
	UserID      int64         `json:"user_id,omitempty"`
	Identity    *RepoIdentity `json:"identity,omitempty"`
	Head        string        `json:"head,omitempty"`
	Dispatched  bool          `json:"dispatched,omitempty"`
	RequestedAt time.Time     `json:"requested_at"`
	State       string        `json:"state"`
	Path        string        `json:"path,omitempty"`
	// Key is the landed key's wire form, KeyNonce the nonce it is bound to.
	Key         string     `json:"key,omitempty"`
	KeyNonce    string     `json:"key_nonce,omitempty"`
	LandedAt    *time.Time `json:"landed_at,omitempty"`
	Cause       Cause      `json:"cause,omitempty"`
	CauseDetail string     `json:"cause_detail,omitempty"`
	Link        string     `json:"link,omitempty"`
	// Checkout and Portal are the links a refusal named (where a plan is
	// picked or managed), "" when it named none.
	Checkout string `json:"checkout_url,omitempty"`
	Portal   string `json:"portal_url,omitempty"`
	// Noticed is the last notice a hook passed on, so it is not repeated.
	Noticed string `json:"noticed,omitempty"`
	// RenewAttempts counts the renewals started since a key last landed.
	RenewAttempts int `json:"renew_attempts,omitempty"`
}

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// SessionsDir is <cache>/sessions.
func SessionsDir(cacheRoot string) string { return filepath.Join(cacheRoot, "sessions") }

// StatePath is <cache>/sessions/<session id>.json, refusing an id that
// could leave the folder.
func StatePath(cacheRoot, sessionID string) (string, error) {
	if !sessionIDRe.MatchString(sessionID) || sessionID == "." || sessionID == ".." {
		return "", fmt.Errorf("session id %q is not a plain name", sessionID)
	}
	return filepath.Join(SessionsDir(cacheRoot), sessionID+".json"), nil
}

// ReadState reads a state file: nil for a missing one (a fresh session),
// an error for one that does not parse.
func ReadState(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.V != 1 || (f.State != StatePending && f.State != StateLanded && f.State != StateDegraded) {
		return nil, fmt.Errorf("%s: version %d state %q", path, f.V, f.State)
	}
	return &f, nil
}

func writeState(path string, f *File) error {
	if err := paths.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	f.V = 1
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return paths.PlaceReadOnly(filepath.Dir(path), filepath.Base(path), append(raw, '\n'), 0o600)
}

// lockState takes path's lock file, breaking one older than five seconds
// (a writer that died holding it).
func lockState(path string) (func(), error) {
	if err := paths.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	lp := path + ".lock"
	for i := 0; i < 400; i++ {
		f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lp) }, nil
		}
		if st, e := os.Stat(lp); e == nil && time.Since(st.ModTime()) > 5*time.Second {
			_ = os.Remove(lp)
			continue
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil, fmt.Errorf("%s is locked", path)
}

// UpdateState reads path under its lock and hands the file (nil when
// missing) to edit; edit's result is written when it returns true.
func UpdateState(path string, edit func(f *File) (*File, bool)) error {
	unlock, err := lockState(path)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := ReadState(path)
	if err != nil {
		f = nil
	}
	next, write := edit(f)
	if !write || next == nil {
		return nil
	}
	return writeState(path, next)
}

// Verdict is what a hook makes of the state file at one moment.
type Verdict struct {
	// Key is the landed key, verified and bound on this read.
	Key *KeyPayload
	// Pending gates every surface on: no key yet, inside the cut.
	Pending bool
	// Cause, Detail and Link say why there is no key.
	Cause  Cause
	Detail string
	Link   string
	// Renew asks for a fresh key: the landed one is a day old and no
	// request is in flight.
	Renew bool
	// PollDue lets a hook make its one read: no key, the request was sent
	// and the tail is not over.
	PollDue bool
	// CutPassed is a pending file past the cut: it should read degraded.
	CutPassed bool
	// TailOver is no key and nothing polling any more.
	TailOver bool
	// NoFile is a session with no state file.
	NoFile bool
}

// Decide reads f at now: the key is re-verified against roots and bound
// to the session on every read, so an expired or tampered key degrades at
// the next hook with no clock of its own.
func Decide(f *File, now time.Time, roots []ed25519.PublicKey) Verdict {
	if f == nil {
		return Verdict{NoFile: true, Cause: CauseStateFile, Detail: "no state file"}
	}
	var v Verdict
	tail := Tail()
	end := f.RequestedAt.Add(Cut + tail)
	inflight := now.Before(end) && (f.State == StatePending || (f.LandedAt != nil && f.RequestedAt.After(*f.LandedAt)))
	keyCause, keyDetail := Cause(""), ""
	if f.Key != "" {
		k, cause, detail := checkKey([]byte(f.Key), roots, now, f)
		if cause == "" {
			v.Key = &k
			if f.LandedAt != nil && now.Sub(*f.LandedAt) >= RenewAge && !inflight && renewDue(f, now) {
				v.Renew = true
			}
			return v
		}
		keyCause, keyDetail = cause, detail
	}
	unanswered := f.Cause == "" || f.Cause == CauseAppNotInstalled || f.Cause == CauseGitHubUnreachable || f.Cause == CauseServerUnreachable
	v.PollDue = now.Before(end) && unanswered && ((f.Mode == PathWeb && f.Dispatched && f.Head != "") || (f.Mode == PathDesktop && f.Identity != nil))
	switch {
	case f.State == StatePending && now.Before(f.RequestedAt.Add(Cut)):
		v.Pending = true
		return v
	case f.State == StatePending:
		v.CutPassed = true
		v.Cause = likeliest(f)
	case keyCause != "":
		v.Cause, v.Detail = keyCause, keyDetail
	default:
		v.Cause, v.Detail, v.Link = f.Cause, f.CauseDetail, f.Link
		if v.Cause == "" {
			v.Cause = likeliest(f)
		}
	}
	if v.Link == "" {
		v.Link = LinkFor(v.Cause)
	}
	v.TailOver = !now.Before(end)
	return v
}

// likeliest is the cut's cause from what the request observed: a
// dispatch GitHub accepted with no check run by the cut means no App
// answered; otherwise the side that did not answer.
func likeliest(f *File) Cause {
	switch {
	case f.Cause != "":
		return f.Cause
	case f.Dispatched:
		return CauseAppNotInstalled
	case f.Mode == PathDesktop:
		return CauseServerUnreachable
	}
	return CauseGitHubUnreachable
}

// checkKey verifies a stored key and binds it to the file's repo, user and
// the nonce it was fetched with.
func checkKey(key []byte, roots []ed25519.PublicKey, now time.Time, f *File) (KeyPayload, Cause, string) {
	k, err := VerifyKey(key, roots, now)
	if err != nil {
		return KeyPayload{}, CauseKeyRefused, string(ReasonOf(err))
	}
	if f.Identity == nil {
		return KeyPayload{}, CauseStateFile, "no repo identity beside the key"
	}
	if c := Bind(k, *f.Identity, &Session{UserID: f.UserID, Nonce: f.KeyNonce}); c != "" {
		return KeyPayload{}, c, bindDetail(c, k, *f.Identity)
	}
	return k, "", ""
}

// bindDetail names a bind mismatch for the notice.
func bindDetail(c Cause, k KeyPayload, repo RepoIdentity) string {
	switch c {
	case CauseBindPlan:
		vis := "public"
		if repo.Private {
			vis = "private"
		}
		return fmt.Sprintf("a %s key for %s on a %s repo of %s", k.Plan, k.OwnerLogin, vis, repo.OwnerLogin)
	case CauseBindRepo:
		return fmt.Sprintf("a key for repo %d in repo %d", k.RepoID, repo.ID)
	case CauseBindUser:
		return "a key for another user"
	case CauseBindNonce:
		return "a key for another session's nonce"
	}
	return string(c)
}
