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
	title := fmt.Sprintf("@claudinite/cli %s fails the post-publish smoke on %s", version, leg)
	tail := logTail(log)
	var b strings.Builder
	fmt.Fprintf(&b, "`@claudinite/cli %s` is published, and the %s leg of `from-npm.yml` failed against registry.npmjs.org: a member pinned to it on %s cannot run its engine.\n\n", version, leg, leg)
	fmt.Fprintf(&b, "While this issue is open with the `release-blocker` label, `promote.yml` refuses to promote %s. Close it once the failure is understood; the fix ships as a new version, since nothing is republished.\n\n", version)
	fmt.Fprintf(&b, "Run: %s\n\nThe last %d lines of the leg's log:\n\n~~~~\n%s\n~~~~\n", runURL, blockerLogLines, tail)
	return title, b.String()
}

// LivePacksBlockerIssue is the release-blocker issue the release's
// live-packs job opens when a candidate, read against the real pack CDN
// and vendored branch, fails dev/release/rehearse.sh --mode live-packs.
func LivePacksBlockerIssue(version, runURL, log string) (string, string) {
	title := fmt.Sprintf("@claudinite/cli %s fails the live-packs rehearsal against the real shelf", version)
	var b strings.Builder
	fmt.Fprintf(&b, "`dev/release/rehearse.sh --mode live-packs` failed for %s: built from this release's source and signed with the development keys, it did not adopt, load, check or update the real packs from packs.claudinite.com and ClaudinitePacks' vendored branch as a member would.\n\n", version)
	fmt.Fprintf(&b, "While this issue is open with the `release-blocker` label, `promote.yml` refuses to promote %s. Close it once the failure is understood: a shelf-side cause is fixed in ClaudinitePacks, an engine-side one ships as a new version.\n\n", version)
	fmt.Fprintf(&b, "Run: %s\n\nThe last %d lines of the rehearsal's log:\n\n~~~~\n%s\n~~~~\n", runURL, blockerLogLines, logTail(log))
	return title, b.String()
}

// logTail is the last blockerLogLines of log, with a fence it carries
// shortened so it cannot close the issue's own.
func logTail(log string) string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	if len(lines) > blockerLogLines {
		lines = lines[len(lines)-blockerLogLines:]
	}
	return strings.ReplaceAll(strings.Join(lines, "\n"), "~~~~", "~~~")
}
