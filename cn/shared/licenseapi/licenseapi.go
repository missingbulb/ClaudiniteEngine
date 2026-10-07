// Package licenseapi is the license Worker's client, the one place its
// route is spelled (ClaudiniteLicenses workers/key/README.md). Only a fleet
// asks it anything:
//
//	POST /v1/actions-key          ActionsKey        (Bearer OIDC token)
//
// CLAUDINITE_LICENSE_API overrides the base for the rehearsal's stub, in a
// devroots build only. The base is HTTPS, or plain HTTP on loopback only;
// every answer is capped at MaxBody and every call at Timeout.
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

// FromEnv is New(DefaultBase); a development build takes
// CLAUDINITE_LICENSE_API instead when it is set.
func FromEnv() (*Client, error) {
	base := DefaultBase
	if baseOverride != "" {
		if b := os.Getenv(baseOverride); b != "" {
			base = b
		}
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

// KeyAnswer is an issued key: its wire form and what the server says of it.
type KeyAnswer struct {
	Key  string `json:"key"`
	Plan string `json:"plan"`
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

// ActionsKey exchanges the job's OIDC token for the run's key, which names
// the repo owner's plan.
func (c *Client) ActionsKey(oidcToken, engine string) (KeyAnswer, error) {
	return c.key("/v1/actions-key", oidcToken, map[string]string{"engine_version": engine})
}
