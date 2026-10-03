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
	title, body := BlockerIssue("1.60928.3", "darwin-arm64", "https://github.com/missingbulb/ClaudiniteEngine/actions/runs/1", log.String())
	if !strings.Contains(title, "1.60928.3") || !strings.Contains(title, "darwin-arm64") {
		t.Errorf("title %q", title)
	}
	if strings.Contains(body, "line 40\n") || !strings.Contains(body, "line 100\n") {
		t.Error("body does not keep only the log's tail")
	}
	golden(t, "blocker-issue.md", title+"\n\n"+body)
}

func TestLivePacksBlockerIssue(t *testing.T) {
	log := "rehearse: live-packs 1: cn init through npx vendored 8 packs\nrehearse: FAIL: live-packs 2: check world on the adopted tree: aws-sam/handler-path\n"
	title, body := LivePacksBlockerIssue("1.61003.2", "https://github.com/missingbulb/ClaudiniteEngine/actions/runs/2", log)
	if !strings.Contains(title, "1.61003.2") || !strings.Contains(title, "live-packs") {
		t.Errorf("title %q", title)
	}
	if !strings.Contains(body, "promote.yml") || !strings.Contains(body, "live-packs 2") {
		t.Errorf("body names no promotion hold or no log:\n%s", body)
	}
	golden(t, "blocker-issue-live-packs.md", title+"\n\n"+body)
}
