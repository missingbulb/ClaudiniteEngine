package release

import (
	"fmt"
	"strings"
)

// blockerLogLines is how much of a failing leg's log an issue carries.
const blockerLogLines = 60

// BlockerIssue is the release-blocker issue the post-publish smoke opens
// when a published version fails on a platform. The promotion gate holds
// any version an open release-blocker issue names in its title or body.
func BlockerIssue(version, leg, runURL, log string) (string, string) {
	title := fmt.Sprintf("@claudinite/cli-rc %s fails the post-publish smoke on %s", version, leg)
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	if len(lines) > blockerLogLines {
		lines = lines[len(lines)-blockerLogLines:]
	}
	tail := strings.ReplaceAll(strings.Join(lines, "\n"), "~~~~", "~~~")
	var b strings.Builder
	fmt.Fprintf(&b, "`@claudinite/cli-rc %s` is published, and the %s leg of `smoke-published` failed against registry.npmjs.org: a member pinned to it on %s cannot run its engine.\n\n", version, leg, leg)
	fmt.Fprintf(&b, "While this issue is open with the `release-blocker` label, `promote.yml` refuses to promote %s. Close it once the failure is understood; the fix ships as a new version, since nothing is republished.\n\n", version)
	fmt.Fprintf(&b, "Run: %s\n\nThe last %d lines of the leg's log:\n\n~~~~\n%s\n~~~~\n", runURL, blockerLogLines, tail)
	return title, b.String()
}
