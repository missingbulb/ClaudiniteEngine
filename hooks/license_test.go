package hooks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
)

// fakeLicense answers with fixed statuses and records the sessions asked.
type fakeLicense struct {
	start, hook LicenseStatus
	asked       []string
}

func (f *fakeLicense) SessionStart(repo, id string) LicenseStatus {
	f.asked = append(f.asked, "start "+id)
	return f.start
}

func (f *fakeLicense) Hook(repo, id string) LicenseStatus {
	f.asked = append(f.asked, "hook "+id)
	return f.hook
}

func TestSessionStartCarriesTheLicenseLineBeforeTheBreadcrumb(t *testing.T) {
	repo := member(t, nil, nil)
	fl := &fakeLicense{start: LicenseStatus{Line: "[cn] license pending: every feature is on while the key arrives", WorkChecks: true}}
	out, _ := hook(t, Handler{ProjectDir: repo, License: fl}, "session-start", `{"session_id":"abc","hook_event_name":"SessionStart","source":"startup"}`)
	var got sessionStartOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(got.HookSpecificOutput.AdditionalContext, "\n"), "\n")
	if lines[len(lines)-2] != fl.start.Line || !crumb.MatchString(lines[len(lines)-1]) {
		t.Fatalf("last lines %q", lines[len(lines)-2:])
	}
	if strings.Index(got.HookSpecificOutput.AdditionalContext, "[cn] packs") > strings.Index(got.HookSpecificOutput.AdditionalContext, "[cn] license") {
		t.Error("the license line comes before the self-check line")
	}
	if len(fl.asked) != 1 || fl.asked[0] != "start abc" {
		t.Errorf("asked %v", fl.asked)
	}
}

func TestStopRunsNoWorkChecksWhenTheGateIsOff(t *testing.T) {
	repo := member(t, nil, nil)
	blocking := CheckResult{Findings: []findings.Finding{{Class: findings.Coded, ID: "hello/hello-check", Path: "HELLO_FINDING", Sentence: "delete it"}}}
	fc := &fakeChecks{result: blocking}
	fl := &fakeLicense{hook: LicenseStatus{WorkChecks: false, State: "degraded"}}
	out, errOut := hook(t, Handler{Checks: fc, License: fl, ProjectDir: repo}, "stop", `{"session_id":"abc","hook_event_name":"Stop"}`)
	if strings.TrimSpace(out) != "{}" || len(fc.ran) != 0 {
		t.Fatalf("stdout %q runs %v", out, fc.ran)
	}
	if !strings.Contains(errOut, "[cn] license: work checks off (degraded)") {
		t.Errorf("stderr %q", errOut)
	}
	fl.hook.WorkChecks = true
	out, _ = hook(t, Handler{Checks: fc, License: fl, ProjectDir: repo}, "stop", `{"session_id":"abc","hook_event_name":"Stop"}`)
	if !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("with the gate on: %q", out)
	}
}

func TestANoticeIsPassedOnAsAdditionalContext(t *testing.T) {
	repo := member(t, nil, nil)
	for event, name := range map[string]string{"pre-tool-use": "PreToolUse", "user-prompt-submit": "UserPromptSubmit", "post-tool-use": "PostToolUse"} {
		fl := &fakeLicense{hook: LicenseStatus{Notice: "[cn] license degraded: tell the person."}}
		out, _ := hook(t, Handler{License: fl, ProjectDir: repo}, event, `{"session_id":"abc"}`)
		var got sessionStartOut
		if err := json.Unmarshal([]byte(out), &got); err != nil || got.HookSpecificOutput.HookEventName != name || got.HookSpecificOutput.AdditionalContext != fl.hook.Notice {
			t.Errorf("%s: %q %v", event, out, err)
		}
		fl.hook.Notice = ""
		if out, _ := hook(t, Handler{License: fl, ProjectDir: repo}, event, `{"session_id":"abc"}`); strings.TrimSpace(out) != "{}" {
			t.Errorf("%s without a notice: %q", event, out)
		}
	}
}

func TestAHookWithoutASessionIDSharesOneState(t *testing.T) {
	fl := &fakeLicense{}
	hook(t, Handler{License: fl, ProjectDir: t.TempDir()}, "pre-tool-use", `{}`)
	if len(fl.asked) != 1 || fl.asked[0] != "hook unknown" {
		t.Errorf("%v", fl.asked)
	}
}
