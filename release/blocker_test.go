package release

import (
	"fmt"
	"strings"
	"testing"
)

func TestBlockerIssue(t *testing.T) {
	var log strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&log, "line %d\n", i)
	}
	log.WriteString("~~~~ a fence in the log\nsmoke-platform: FAIL: selftest version: version 0.0.0\n")
	title, body := BlockerIssue("60928.3.0", "darwin-arm64", "https://github.com/missingbulb/ClaudiniteEngine/actions/runs/1", log.String())
	if !strings.Contains(title, "60928.3.0") || !strings.Contains(title, "darwin-arm64") {
		t.Errorf("title %q", title)
	}
	if strings.Contains(body, "line 40\n") || !strings.Contains(body, "line 100\n") {
		t.Error("body does not keep only the log's tail")
	}
	golden(t, "blocker-issue.md", title+"\n\n"+body)
}
