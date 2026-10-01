package githubapi

// The calls a session makes for its license key, with the person's own
// credential: on a Claude Code web VM the proxy adds it, so the client
// sends a token only when the environment holds one.
//
//	GET   /user                                              User
//	GET   /repos/{repo}                                      RepoInfo
//	GET   /repos/{repo}/commits/{branch}                     BranchHead
//	POST  /repos/{repo}/dispatches                           RepositoryDispatch
//	GET   /repos/{repo}/commits/{sha}/check-runs?check_name= CheckRuns
//
// Besides the REST calls: the Actions OIDC token, the Actions event
// payload, a GitHub remote URL, and GitHub's device flow for cn login.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// SessionTimeout bounds each session call.
const SessionTimeout = 5 * time.Second

// SessionFromEnv builds a client for repo (owner/name) with the token in
// GH_TOKEN or GITHUB_TOKEN when one is set, and CLAUDINITE_GITHUB_API.
// The transport is Go's default, which honours HTTPS_PROXY and
// SSL_CERT_FILE.
func SessionFromEnv(repo string) *Client {
	c := &Client{Base: DefaultBase, Repo: repo, HTTP: &http.Client{Timeout: SessionTimeout}}
	if b := os.Getenv("CLAUDINITE_GITHUB_API"); b != "" {
		c.Base = strings.TrimRight(b, "/")
	}
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := os.Getenv(k); v != "" {
			c.Token = v
			break
		}
	}
	return c
}

// Account is a user or an organization.
type Account struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}

// User reads the account the credential belongs to.
func (c *Client) User() (Account, error) {
	var u Account
	err := c.do(http.MethodGet, "/user", nil, &u)
	return u, err
}

// RepoInfo is a repository as GitHub describes it to the caller.
type RepoInfo struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	FullName      string  `json:"full_name"`
	Private       bool    `json:"private"`
	DefaultBranch string  `json:"default_branch"`
	Owner         Account `json:"owner"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

// RepoInfo reads the client's repository.
func (c *Client) RepoInfo() (RepoInfo, error) {
	var r RepoInfo
	err := c.do(http.MethodGet, "/repos/"+c.Repo, nil, &r)
	return r, err
}

// BranchHead is the commit sha at the tip of branch on GitHub.
func (c *Client) BranchHead(branch string) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	err := c.do(http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s", c.Repo, url.PathEscape(branch)), nil, &out)
	if err == nil && out.SHA == "" {
		err = fmt.Errorf("GET /repos/%s/commits/%s: no sha", c.Repo, branch)
	}
	return out.SHA, err
}

// RepositoryDispatch sends a repository_dispatch of eventType.
func (c *Client) RepositoryDispatch(eventType string, payload any) error {
	return c.do(http.MethodPost, "/repos/"+c.Repo+"/dispatches", map[string]any{"event_type": eventType, "client_payload": payload}, nil)
}

// CheckRun is one check run, with the output fields a key travels in.
type CheckRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ExternalID string `json:"external_id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	} `json:"output"`
	App struct {
		Slug string `json:"slug"`
	} `json:"app"`
}

// CheckRuns lists the check runs named name on commit sha.
func (c *Client) CheckRuns(sha, name string) ([]CheckRun, error) {
	var out struct {
		Runs []CheckRun `json:"check_runs"`
	}
	q := url.Values{"check_name": {name}, "per_page": {"100"}}
	err := c.do(http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s/check-runs?%s", c.Repo, url.PathEscape(sha), q.Encode()), nil, &out)
	return out.Runs, err
}

var (
	remoteRe = []*regexp.Regexp{
		regexp.MustCompile(`^https://(?:[^@/]+@)?github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
		regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`),
		regexp.MustCompile(`^ssh://git@github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
		regexp.MustCompile(`^git://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
	}
	ownerRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// ParseRemote is owner/name for a GitHub remote URL in any of its four
// shapes (https, scp-like ssh, ssh and git), and false for anything else.
func ParseRemote(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	for _, re := range remoteRe {
		if m := re.FindStringSubmatch(remote); m != nil && ownerRe.MatchString(m[1]) && nameRe.MatchString(m[2]) {
			return m[1] + "/" + m[2], true
		}
	}
	return "", false
}

// ErrNoOIDC is a job that may not request an OIDC token: the workflow
// lacks id-token: write.
var ErrNoOIDC = errors.New("no OIDC token: ACTIONS_ID_TOKEN_REQUEST_URL and ACTIONS_ID_TOKEN_REQUEST_TOKEN are unset (the job needs permissions: id-token: write)")

// OIDCToken requests the job's OIDC token for audience from
// ACTIONS_ID_TOKEN_REQUEST_URL with the bearer
// ACTIONS_ID_TOKEN_REQUEST_TOKEN, as getenv reads them.
func OIDCToken(h *http.Client, getenv func(string) string, audience string) (string, error) {
	raw, tok := getenv("ACTIONS_ID_TOKEN_REQUEST_URL"), getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if raw == "" || tok == "" {
		return "", ErrNoOIDC
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return "", errors.New("ACTIONS_ID_TOKEN_REQUEST_URL is not an HTTPS URL")
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "bearer "+tok)
	resp, err := h.Do(req)
	if err != nil {
		return "", fmt.Errorf("OIDC token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("OIDC token: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OIDC token: %s", resp.Status)
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Value == "" {
		return "", errors.New("OIDC token: the answer holds no value")
	}
	return out.Value, nil
}

// EventRepo is the repository an Actions event payload describes.
type EventRepo struct {
	ID      int64   `json:"id"`
	Private bool    `json:"private"`
	Owner   Account `json:"owner"`
}

// ReadEvent reads the repository from the event payload at path
// (GITHUB_EVENT_PATH).
func ReadEvent(path string) (EventRepo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return EventRepo{}, err
	}
	var ev struct {
		Repository *EventRepo `json:"repository"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return EventRepo{}, fmt.Errorf("%s: %w", path, err)
	}
	if ev.Repository == nil || ev.Repository.ID == 0 || ev.Repository.Owner.ID == 0 {
		return EventRepo{}, fmt.Errorf("%s names no repository", path)
	}
	return *ev.Repository, nil
}

// Device is GitHub's answer to a device code request.
type Device struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
}

// Token is GitHub's answer to a token request: a user access token, or
// Error naming why not (authorization_pending, slow_down, expired_token,
// access_denied, ...).
type Token struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	Interval              int64  `json:"interval"`
}

func formPost(h *http.Client, endpoint string, params url.Values, out any) error {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && (u.Scheme != "http" || !loopbackHost(u.Hostname()))) {
		return fmt.Errorf("the device flow runs over HTTPS only (HTTP on loopback), not %s", endpoint)
	}
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %s", u.Path, resp.Status)
	}
	return json.Unmarshal(body, out)
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DeviceCode starts the device flow for clientID.
func DeviceCode(h *http.Client, endpoint, clientID string) (Device, error) {
	var d Device
	if err := formPost(h, endpoint, url.Values{"client_id": {clientID}}, &d); err != nil {
		return Device{}, err
	}
	if d.DeviceCode == "" || d.UserCode == "" || d.VerificationURI == "" {
		return Device{}, errors.New("the device code answer is incomplete")
	}
	return d, nil
}

// DeviceToken polls once for the device flow's token; an answer naming an
// error is returned with a non-nil error.
func DeviceToken(h *http.Client, endpoint, clientID, deviceCode string) (Token, error) {
	var t Token
	err := formPost(h, endpoint, url.Values{"client_id": {clientID}, "device_code": {deviceCode},
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &t)
	if err != nil {
		return Token{}, err
	}
	if t.Error != "" {
		return t, errors.New(t.Error)
	}
	if t.AccessToken == "" {
		return t, errors.New("the token answer holds no access_token")
	}
	return t, nil
}
