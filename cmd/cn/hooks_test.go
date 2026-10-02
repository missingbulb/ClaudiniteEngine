package main

import (
	"strings"
	"testing"
)

// A PreToolUse block exits 2 with the reason first on stderr, which is
// what Claude Code reads as the denial; anything else exits 0.
func TestHookBlockExitsTwo(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())
	del := `{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git push origin --delete x"}}`
	out, errOut, code := runInProc([]string{"hook", "pre-tool-use"}, del)
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "Blocked: never delete a remote branch") || !strings.Contains(errOut, "[cn] hooks pre-tool-use block ") {
		t.Errorf("exit %d stdout %q stderr %q", code, out, errOut)
	}
	if strings.Contains(errOut, "cn: block") {
		t.Errorf("a block printed an error line: %q", errOut)
	}
	ls := `{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`
	out, errOut, code = runInProc([]string{"hook", "pre-tool-use"}, ls)
	if code != 0 || strings.TrimSpace(out) != "{}" {
		t.Errorf("exit %d stdout %q stderr %q", code, out, errOut)
	}
}
