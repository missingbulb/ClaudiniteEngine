package execute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
)

// The endpoint a task names when it names none, and the secret holding
// that endpoint's token when the member names none.
const (
	DefaultEndpoint    = "default"
	DefaultTokenSecret = "CCR_ROUTINE_TOKEN"
	// EndpointsKey is the claudinite-tasks config key mapping endpoint
	// names to routines.
	EndpointsKey = "agenticTaskInvocationEndpoints"
	// FireTimeout bounds the one fire.
	FireTimeout = 60 * time.Second
)

// DefaultHeaders are the routine API's dated headers; a member's endpoint
// entry may override them.
var DefaultHeaders = map[string]string{
	"anthropic-beta":    "experimental-cc-routine-2026-04-01",
	"anthropic-version": "2023-06-01",
}

// Endpoint is a routine trigger a hand-off fires, or why it cannot.
type Endpoint struct {
	Name, URL, TokenSecret string
	Headers                map[string]string
	Error                  string
}

// ResolveEndpoint is the task's endpoint in the member's map. A task names
// a key, never a URL; the member maps it to the routine's trigger and the
// name of the secret holding its token.
func ResolveEndpoint(endpoints map[string]any, t taskspec.Task) Endpoint {
	name, _ := t.Decl.Str("invocation_endpoint")
	if name == "" {
		name = DefaultEndpoint
	}
	e := Endpoint{Name: name}
	entry, ok := endpoints[name].(map[string]any)
	if !ok {
		e.Error = fmt.Sprintf("this repo's settings declare no invocation endpoint %q (the claudinite-tasks config's %s)", name, EndpointsKey)
		return e
	}
	raw, _ := entry["url"].(string)
	if raw == "" {
		e.Error = fmt.Sprintf("invocation endpoint %q declares no url", name)
		return e
	}
	if u, err := url.Parse(raw); err != nil || u.Scheme != "https" || u.Host == "" {
		e.Error = fmt.Sprintf("invocation endpoint %q is not an https URL", name)
		return e
	}
	e.URL = strings.TrimRight(raw, "/")
	if !strings.HasSuffix(e.URL, "/fire") {
		e.URL += "/fire"
	}
	e.TokenSecret, _ = entry["tokenSecret"].(string)
	if e.TokenSecret == "" && name == DefaultEndpoint {
		e.TokenSecret = DefaultTokenSecret
	}
	if e.TokenSecret == "" {
		e.Error = fmt.Sprintf("invocation endpoint %q declares no tokenSecret (the NAME of the repo Actions secret holding its token)", name)
		return e
	}
	e.Headers = map[string]string{}
	for k, v := range DefaultHeaders {
		e.Headers[k] = v
	}
	if h, ok := entry["headers"].(map[string]any); ok {
		for k, v := range h {
			if s, ok := v.(string); ok {
				e.Headers[k] = s
			}
		}
	}
	return e
}

// FirePayload names the item and the nonce, as prose, and nothing else:
// the routine's stored prompt says what to do with it.
func FirePayload(repo string, item int, nonce string) string {
	return fmt.Sprintf("Claudinite work item: %s#%d. Invocation nonce: %s.", repo, item, nonce)
}

// Invoker fires a task's routine, once per item and never again: the
// endpoint offers no idempotency key, so an outcome nobody learned is
// unknown, left to the agent leash.
type Invoker struct {
	Repo      string
	Endpoints map[string]any
	Env       map[string]string
	HTTP      *http.Client
	Timeout   time.Duration
}

// Invoke is the one fire.
func (iv Invoker) Invoke(t taskspec.Task, item workitem.Issue, nonce string) Invocation {
	e := ResolveEndpoint(iv.Endpoints, t)
	if e.Error != "" {
		return Invocation{Answered: true, Error: e.Error}
	}
	token, _ := secretValue(e.TokenSecret, iv.Env, parseBag(iv.Env[SecretsBagEnv]))
	if token == "" {
		return Invocation{Answered: true, Error: fmt.Sprintf("`%s`, the token for invocation endpoint %q, is empty in this job. Either the repository secret is not set, or `.github/workflows/claudinite-executor.yml` does not pass it — check that this repo's executor workflow names it under the `# claudinite:secrets` marker", e.TokenSecret, e.Name)}
	}
	body, _ := json.Marshal(map[string]string{"text": FirePayload(iv.Repo, item.Number, nonce)})
	req, err := http.NewRequest(http.MethodPost, e.URL, bytes.NewReader(body))
	if err != nil {
		return Invocation{Answered: true, Error: fmt.Sprintf("endpoint %q: %v", e.Name, err)}
	}
	for k, v := range e.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "claudinite-executor")
	client := iv.HTTP
	if client == nil {
		client = &http.Client{}
	}
	timeout := iv.Timeout
	if timeout <= 0 {
		timeout = FireTimeout
	}
	c := *client
	c.Timeout = timeout
	resp, err := c.Do(req)
	if err != nil {
		return Invocation{Error: fmt.Sprintf("endpoint %q gave no answer: %v", e.Name, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var answer struct {
		SessionID  string          `json:"claude_code_session_id"`
		SessionURL string          `json:"claude_code_session_url"`
		Error      json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return Invocation{OK: true, Answered: true, SessionID: answer.SessionID, SessionURL: answer.SessionURL}
	}
	msg := fmt.Sprintf("endpoint %q returned %d", e.Name, resp.StatusCode)
	if len(answer.Error) > 0 && string(answer.Error) != "null" {
		msg += ": " + string(answer.Error)
	}
	return Invocation{Answered: true, Error: msg}
}

// GrantComment posts an item's grant, in its wire form, on the item.
func GrantComment(grant string) string {
	return workitem.GrantMarker + "\nThis item's grant from the license server. The routine session verifies it before acting and never takes a seat.\n\n```json\n" + grant + "\n```"
}

// GrantFromComment reads the grant's wire form back off its comment.
func GrantFromComment(body string) (string, bool) {
	if !strings.HasPrefix(body, workitem.GrantMarker) {
		return "", false
	}
	_, rest, ok := strings.Cut(body, "```json\n")
	if !ok {
		return "", false
	}
	grant, _, ok := strings.Cut(rest, "\n```")
	return grant, ok
}
