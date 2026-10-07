package execute

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
)

func TestAnEndpointResolvesFromTheMembersMap(t *testing.T) {
	tk := loopTask("a", nil)
	endpoints := map[string]any{"default": map[string]any{"url": "https://api.example/v1/claude_code/routines/trig_1"}}
	e := ResolveEndpoint(endpoints, tk)
	if e.Error != "" || e.TokenSecret != DefaultTokenSecret || e.URL != "https://api.example/v1/claude_code/routines/trig_1/fire" {
		t.Errorf("%+v", e)
	}
	if e.Headers["anthropic-beta"] == "" || e.Headers["anthropic-version"] == "" {
		t.Error(e.Headers)
	}
	endpoints["default"] = map[string]any{"url": "https://x/fire", "tokenSecret": "MY_TOKEN", "headers": map[string]any{"anthropic-beta": "newer"}}
	e = ResolveEndpoint(endpoints, tk)
	if e.URL != "https://x/fire" || e.TokenSecret != "MY_TOKEN" || e.Headers["anthropic-beta"] != "newer" {
		t.Errorf("%+v", e)
	}
	acme := loopTask("a", map[string]any{"invocation_endpoint": "acme"})
	if e := ResolveEndpoint(endpoints, acme); !strings.Contains(e.Error, `no invocation endpoint "acme"`) {
		t.Error(e)
	}
	endpoints["acme"] = map[string]any{"url": "https://y"}
	if e := ResolveEndpoint(endpoints, acme); !strings.Contains(e.Error, "tokenSecret") {
		t.Error("only default has a default secret:", e)
	}
	if e := ResolveEndpoint(map[string]any{"default": map[string]any{}}, tk); !strings.Contains(e.Error, "declares no url") {
		t.Error(e)
	}
	if e := ResolveEndpoint(map[string]any{"default": map[string]any{"url": "http://plain"}}, tk); !strings.Contains(e.Error, "https") {
		t.Error("a routine is fired over HTTPS:", e)
	}
}

func TestThePayloadNamesTheItemAndTheNonceAndNothingElse(t *testing.T) {
	if got := FirePayload("o/r", 12, "12-abc"); got != "Claudinite work item: o/r#12. Invocation nonce: 12-abc." {
		t.Error(got)
	}
}

type fired struct {
	auth, beta, path string
	body             map[string]any
}

func routine(t *testing.T, status int, answer string) (*httptest.Server, *[]fired) {
	var calls []fired
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		calls = append(calls, fired{r.Header.Get("Authorization"), r.Header.Get("anthropic-beta"), r.URL.Path, body})
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func invoker(srv *httptest.Server, env map[string]string) Invoker {
	return Invoker{Repo: "o/r", Endpoints: map[string]any{"default": map[string]any{"url": srv.URL + "/v1/claude_code/routines/trig_1"}},
		Env: env, HTTP: srv.Client(), Timeout: 5 * time.Second}
}

func TestAFireCallsOnceWithTheBearerAndTheItem(t *testing.T) {
	srv, calls := routine(t, 200, `{"claude_code_session_id":"s-9","claude_code_session_url":"https://claude.ai/code/s-9"}`)
	inv := invoker(srv, map[string]string{"CLAUDINITE_SECRETS": `{"CCR_ROUTINE_TOKEN":"sk-r"}`}).Invoke(loopTask("a", nil), workitem.Issue{Number: 12}, "12-abc")
	if !inv.OK || inv.SessionID != "s-9" || inv.SessionURL != "https://claude.ai/code/s-9" {
		t.Fatalf("%+v", inv)
	}
	if len(*calls) != 1 {
		t.Fatal(*calls)
	}
	c := (*calls)[0]
	if c.auth != "Bearer sk-r" || c.path != "/v1/claude_code/routines/trig_1/fire" || c.beta == "" {
		t.Errorf("%+v", c)
	}
	if len(c.body) != 1 || c.body["text"] != "Claudinite work item: o/r#12. Invocation nonce: 12-abc." {
		t.Errorf("%+v", c.body)
	}
}

func TestARefusedFireIsAnsweredAndNamesTheEndpoint(t *testing.T) {
	srv, calls := routine(t, 401, `{"error":{"type":"auth"}}`)
	inv := invoker(srv, map[string]string{"CCR_ROUTINE_TOKEN": "sk-r"}).Invoke(loopTask("a", nil), workitem.Issue{Number: 12}, "n")
	if inv.OK || !inv.Answered || !strings.Contains(inv.Error, `endpoint "default" returned 401`) || len(*calls) != 1 {
		t.Errorf("%+v", inv)
	}
}

func TestAnEmptyTokenNamesTheSecretAndFiresNothing(t *testing.T) {
	srv, calls := routine(t, 200, `{}`)
	inv := invoker(srv, map[string]string{"CCR_ROUTINE_TOKEN": ""}).Invoke(loopTask("a", nil), workitem.Issue{Number: 12}, "n")
	if inv.OK || !inv.Answered || !strings.Contains(inv.Error, "CCR_ROUTINE_TOKEN") || !strings.Contains(inv.Error, `"default"`) || len(*calls) != 0 {
		t.Errorf("%+v %v", inv, *calls)
	}
}

func TestAFireWithNoAnswerIsUnknownAndNeverRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(300 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	iv := invoker(srv, map[string]string{"CCR_ROUTINE_TOKEN": "sk"})
	iv.Timeout = 50 * time.Millisecond
	inv := iv.Invoke(loopTask("a", nil), workitem.Issue{Number: 12}, "n")
	if inv.OK || inv.Answered || !strings.Contains(inv.Error, "gave no answer") || hits.Load() != 1 {
		t.Errorf("%+v hits %d", inv, hits.Load())
	}
}

// A routine's name is any key of the member's map, "default" only when the
// task names none: a task naming "not-default" fires that routine with its
// own token, and the default routine is never called.
func TestATaskNamingARoutineFiresThatOneNotTheDefault(t *testing.T) {
	srv, calls := routine(t, 200, `{"claude_code_session_id":"s-1"}`)
	iv := Invoker{Repo: "o/r", HTTP: srv.Client(), Timeout: 5 * time.Second,
		Endpoints: map[string]any{
			"default":     map[string]any{"url": srv.URL + "/v1/claude_code/routines/trig_default"},
			"not-default": map[string]any{"url": srv.URL + "/v1/claude_code/routines/trig_other", "tokenSecret": "OTHER_ROUTINE_TOKEN"},
		},
		Env: map[string]string{"CLAUDINITE_SECRETS": `{"CCR_ROUTINE_TOKEN":"sk-default","OTHER_ROUTINE_TOKEN":"sk-other"}`}}
	inv := iv.Invoke(loopTask("a", map[string]any{"invocation_endpoint": "not-default"}), workitem.Issue{Number: 7}, "7-n")
	if !inv.OK || len(*calls) != 1 {
		t.Fatalf("%+v %+v", inv, *calls)
	}
	if c := (*calls)[0]; c.path != "/v1/claude_code/routines/trig_other/fire" || c.auth != "Bearer sk-other" {
		t.Errorf("fired %+v, want the not-default routine with its own token", c)
	}
	inv = iv.Invoke(loopTask("b", nil), workitem.Issue{Number: 8}, "8-n")
	if c := (*calls)[len(*calls)-1]; !inv.OK || c.path != "/v1/claude_code/routines/trig_default/fire" || c.auth != "Bearer sk-default" {
		t.Errorf("a task naming no routine fired %+v, want the default", c)
	}
}
