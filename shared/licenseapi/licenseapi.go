// Package licenseapi is the license Worker's client, the one place its
// routes are spelled (ClaudiniteLicenses workers/key/README.md and
// workers/public-key/README.md):
//
//	GET  /v1/login/config         LoginConfig
//	POST /v1/login/refresh        Refresh
//	POST /v1/session-key          SessionKey        (Bearer App user token)
//	POST /v1/public/session-key   PublicSessionKey  (Bearer App user token)
//	POST /v1/actions-key          ActionsKey        (Bearer OIDC token)
//	POST /v1/item-grant           ItemGrant         (Bearer Actions key)
//
// CLAUDINITE_LICENSE_API overrides the base for the rehearsal's stub. The
// base is HTTPS, or plain HTTP on loopback only; every answer is capped at
// MaxBody and every call at Timeout.
package licenseapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultBase is the license server.
	DefaultBase = "https://license.claudinite.com"
	// MaxBody caps an answer.
	MaxBody = 64 << 10
	// Timeout bounds one call.
	Timeout = 5 * time.Second
)

// Client calls the license Worker.
type Client struct {
	Base string
	HTTP *http.Client
}

// New is a client for base, refusing a base that is neither HTTPS nor
// loopback.
func New(base string) (*Client, error) {
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	switch {
	case u.Scheme == "https" && u.Host != "":
	case u.Scheme == "http" && loopback(u.Hostname()):
	default:
		return nil, fmt.Errorf("the license server is called over HTTPS only, not %s", base)
	}
	return &Client{Base: base, HTTP: &http.Client{Timeout: Timeout}}, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// FromEnv is New(CLAUDINITE_LICENSE_API, else DefaultBase).
func FromEnv() (*Client, error) {
	base := os.Getenv("CLAUDINITE_LICENSE_API")
	if base == "" {
		base = DefaultBase
	}
	return New(base)
}

// Refusal is an answer outside 2xx; Reason is the body's "refused" field,
// empty when it has none.
type Refusal struct {
	Status int
	Reason string
}

func (r *Refusal) Error() string {
	if r.Reason == "" {
		return fmt.Sprintf("the license server answered %d", r.Status)
	}
	return fmt.Sprintf("the license server refused (%d %s)", r.Status, r.Reason)
}

// IsUnreachable reports an error that never got an answer from the server.
func IsUnreachable(err error) bool {
	var r *Refusal
	return err != nil && !errors.As(err, &r) && !errors.Is(err, errCap) && !errors.Is(err, errShape)
}

var (
	errCap   = errors.New("the answer is larger than the cap")
	errShape = errors.New("the answer is not the expected JSON")
)

func (c *Client) call(method, path, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.Base+path, body)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if len(raw) > MaxBody {
		return fmt.Errorf("%s %s: %w (%d bytes)", method, path, errCap, MaxBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var r struct {
			Refused string `json:"refused"`
		}
		_ = json.Unmarshal(raw, &r)
		return &Refusal{Status: resp.StatusCode, Reason: r.Refused}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: %w: %v", method, path, errShape, err)
	}
	return nil
}

// LoginConfig is what cn login needs to run GitHub's device flow.
type LoginConfig struct {
	ClientID      string `json:"client_id"`
	DeviceCodeURL string `json:"device_code_url"`
	TokenURL      string `json:"token_url"`
}

// LoginConfig reads the App's client id and the device flow URLs.
func (c *Client) LoginConfig() (LoginConfig, error) {
	var out LoginConfig
	if err := c.call(http.MethodGet, "/v1/login/config", "", nil, &out); err != nil {
		return LoginConfig{}, err
	}
	if out.ClientID == "" || out.DeviceCodeURL == "" || out.TokenURL == "" {
		return LoginConfig{}, fmt.Errorf("GET /v1/login/config: %w: a field is missing", errShape)
	}
	return out, nil
}

// TokenAnswer is GitHub's refresh answer, returned verbatim by the Worker.
type TokenAnswer struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
}

// Refresh exchanges a refresh token for a new App user token.
func (c *Client) Refresh(refreshToken string) (TokenAnswer, error) {
	var out TokenAnswer
	if err := c.call(http.MethodPost, "/v1/login/refresh", "", map[string]string{"refresh_token": refreshToken}, &out); err != nil {
		return TokenAnswer{}, err
	}
	if out.AccessToken == "" {
		reason := out.Error
		if reason == "" {
			reason = "no access_token"
		}
		return TokenAnswer{}, &Refusal{Status: http.StatusUnauthorized, Reason: reason}
	}
	return out, nil
}

// KeyAnswer is an issued key: its wire form and what the server says of it.
type KeyAnswer struct {
	Key   string `json:"key"`
	Plan  string `json:"plan"`
	State string `json:"state"`
}

func (c *Client) key(path, bearer string, in any) (KeyAnswer, error) {
	var out KeyAnswer
	if err := c.call(http.MethodPost, path, bearer, in, &out); err != nil {
		return KeyAnswer{}, err
	}
	if out.Key == "" {
		return KeyAnswer{}, fmt.Errorf("POST %s: %w: no key", path, errShape)
	}
	return out, nil
}

// SessionKey asks the paid key Worker for a desktop session's key.
func (c *Client) SessionKey(userToken, repo, nonce, engine string) (KeyAnswer, error) {
	return c.key("/v1/session-key", userToken, map[string]string{"repo": repo, "nonce": nonce, "engine_version": engine})
}

// PublicSessionKey asks the public key Worker for a desktop session's key.
func (c *Client) PublicSessionKey(userToken, repo, nonce, engine string) (KeyAnswer, error) {
	return c.key("/v1/public/session-key", userToken, map[string]string{"repo": repo, "nonce": nonce, "engine_version": engine})
}

// ActionsKey exchanges the job's OIDC token for the run's key.
func (c *Client) ActionsKey(oidcToken, engine string) (KeyAnswer, error) {
	return c.key("/v1/actions-key", oidcToken, map[string]string{"engine_version": engine})
}

// ItemGrant asks for the grant of a work item's issue with the run's
// Actions key, in its wire form, and answers the grant's wire form.
func (c *Client) ItemGrant(actionsKey string, issue int) (string, error) {
	var out struct {
		Grant string `json:"grant"`
	}
	if err := c.call(http.MethodPost, "/v1/item-grant", actionsKey, map[string]int{"issue": issue}, &out); err != nil {
		return "", err
	}
	if out.Grant == "" {
		return "", fmt.Errorf("POST /v1/item-grant: %w: no grant", errShape)
	}
	return out.Grant, nil
}
