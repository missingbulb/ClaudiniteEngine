package release

import (
	"encoding/json"
	"strings"
)

// ModeInput is what a publish job knows when it decides whether to publish.
type ModeInput struct {
	Channel     string // "rc" or "stable"
	Signing     string // "release" or "dev", from the sign job
	DryRunInput bool   // the dispatch's dry_run input
	// NpmVersions is the output of `npm view <channel package> versions
	// --json`, empty when the command failed.
	NpmVersions string
	// StableTest is "pass" or "fail" for `go test -tags stable ./license`
	// at the candidate's commit; the stable channel requires "pass".
	StableTest string
}

// Mode is "real", "dry-run" or "refuse", with the line that says why.
type Mode struct {
	Name   string
	Notice string
}

// PublishMode derives the publish mode every run; nothing configures it.
// Publishing is real only when the candidate carries the release key's
// signature, nobody asked for a dry run, and npm already holds the
// package, whose trusted publisher #2 attaches.
func PublishMode(in ModeInput) Mode {
	switch in.Channel {
	case "rc":
	case "stable":
		if in.StableTest != "pass" {
			return Mode{"refuse", "go test -tags stable ./license does not pass at this commit: no stable release can exist while the development roots are embedded; see #5"}
		}
	default:
		return Mode{"refuse", "unknown channel " + in.Channel}
	}
	switch in.Signing {
	case "release":
	case "dev":
		return Mode{"dry-run", "signed with the development key, which is never published for real; see #5"}
	default:
		return Mode{"refuse", "unknown signing " + in.Signing}
	}
	if in.DryRunInput {
		return Mode{"dry-run", "the dispatch asked for dry_run"}
	}
	if !reserved(in.NpmVersions) {
		return Mode{"dry-run", "package not reserved; see #2"}
	}
	return Mode{"real", ""}
}

// reserved reads npm view's JSON: a list of versions, or one version as a
// string when there is only one; an error object or nothing means absent.
func reserved(npmView string) bool {
	s := strings.TrimSpace(npmView)
	var list []string
	if json.Unmarshal([]byte(s), &list) == nil {
		return len(list) > 0
	}
	var one string
	if json.Unmarshal([]byte(s), &one) == nil {
		return one != ""
	}
	return false
}
