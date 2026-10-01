package license

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
)

// Store is what a desktop keeps under <cache>/license, 0600 in a 0700
// folder: the App user token cn login obtained, and the last key per repo.
// Nothing else is kept.
type Store struct {
	// Root is the cache root, .../claudinite.
	Root string
}

// Login is the App user token and its refresh token, as GitHub answered
// them, with when they were obtained and whom they name.
type Login struct {
	AccessToken           string    `json:"access_token"`
	ExpiresIn             int64     `json:"expires_in"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresIn int64     `json:"refresh_token_expires_in"`
	ObtainedAt            time.Time `json:"obtained_at"`
	UserID                int64     `json:"user_id,omitempty"`
	UserLogin             string    `json:"user_login,omitempty"`
}

// Valid reports whether the access token is unexpired at now; a token
// with no stated lifetime never expires by the clock.
func (l Login) Valid(now time.Time) bool {
	return l.AccessToken != "" && (l.ExpiresIn <= 0 || now.Before(l.ObtainedAt.Add(time.Duration(l.ExpiresIn)*time.Second)))
}

func (s Store) dir() string { return filepath.Join(s.Root, "license") }

// LoginPath is <cache>/license/login.json.
func (s Store) LoginPath() string { return filepath.Join(s.dir(), "login.json") }

func (s Store) write(rel string, v any) error {
	dir := filepath.Dir(filepath.Join(s.dir(), rel))
	if err := paths.EnsurePrivateDir(s.dir()); err != nil {
		return err
	}
	if err := paths.EnsurePrivateDir(dir); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return paths.PlaceReadOnly(dir, filepath.Base(rel), append(raw, '\n'), 0o600)
}

// ReadLogin is the stored login, nil when there is none.
func (s Store) ReadLogin() (*Login, error) {
	raw, err := os.ReadFile(s.LoginPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l Login
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// WriteLogin stores l.
func (s Store) WriteLogin(l Login) error { return s.write("login.json", l) }

// DeleteLogin removes the stored login; a missing one is not an error.
func (s Store) DeleteLogin() error {
	err := os.Remove(s.LoginPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// CachedKey is a desktop's last key for one repo, with what binding it
// needs offline.
type CachedKey struct {
	Key       string       `json:"key"`
	FetchedAt time.Time    `json:"fetched_at"`
	UserID    int64        `json:"user_id"`
	Nonce     string       `json:"nonce"`
	Identity  RepoIdentity `json:"identity"`
}

// KeyPath is <cache>/license/keys/<first 16 hex of SHA-256 of origin>.json.
func (s Store) KeyPath(origin string) string {
	sum := sha256.Sum256([]byte(origin))
	return filepath.Join(s.dir(), "keys", hex.EncodeToString(sum[:])[:16]+".json")
}

// WriteKey caches a desktop key for origin.
func (s Store) WriteKey(origin string, k CachedKey) error {
	rel, _ := filepath.Rel(s.dir(), s.KeyPath(origin))
	return s.write(rel, k)
}

// Key is the cached key for origin while it verifies at now, is still
// unexpired (keys last 7 days, the design's offline window) and names
// user; ok is false otherwise.
func (s Store) Key(origin string, user int64, roots []ed25519.PublicKey, now time.Time) (CachedKey, KeyPayload, bool) {
	raw, err := os.ReadFile(s.KeyPath(origin))
	if err != nil {
		return CachedKey{}, KeyPayload{}, false
	}
	var c CachedKey
	if json.Unmarshal(raw, &c) != nil || (user != 0 && c.UserID != user) {
		return CachedKey{}, KeyPayload{}, false
	}
	k, err := VerifyKey([]byte(c.Key), roots, now)
	if err != nil || Bind(k, c.Identity, &Session{UserID: c.UserID, Nonce: c.Nonce}) != "" {
		return CachedKey{}, KeyPayload{}, false
	}
	return c, k, true
}
