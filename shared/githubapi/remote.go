package githubapi

// A GitHub remote URL, and an Actions job's OIDC token.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

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
