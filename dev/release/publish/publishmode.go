package publish

import (
	"encoding/json"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/dev/release"
)

// ModeInput is what a publish job knows when it decides whether to publish.
type ModeInput struct {
	Tag         string // the dist-tag the release publishes under: "rc" or "staging"
	Signing     string // "release", once the signature verified against cn/shared/trust/roots
	DryRunInput bool   // the dispatch's dry_run input
	// NpmVersions is the output of `npm view @claudinite/cli versions
	// --json`, empty when the command failed.
	NpmVersions string
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
	switch in.Tag {
	case release.TagRC, release.TagStaging:
	case release.TagLatest:
		return Mode{"refuse", "nothing publishes under latest: promote.yml moves it onto an rc version it verified"}
	default:
		return Mode{"refuse", "unknown dist-tag " + in.Tag}
	}
	switch in.Signing {
	case "release":
	default:
		return Mode{"refuse", "unknown signing " + in.Signing}
	}
	if in.DryRunInput {
		return Mode{"dry-run", "the dispatch asked for dry_run"}
	}
	ok, failure := reserved(in.NpmVersions)
	if failure != "" {
		return Mode{"dry-run", "npm view failed (" + failure + "), so whether the package is reserved is unknown; dispatch again once the registry answers"}
	}
	if !ok {
		return Mode{"dry-run", "package not reserved; see #2"}
	}
	return Mode{"real", ""}
}

// reserved reads npm view's JSON: a list of versions, or one version as a
// string when there is only one. An E404 error object or nothing means
// absent; any other error object is returned as the failure, since it says
// nothing about the package.
func reserved(npmView string) (bool, string) {
	s := strings.TrimSpace(npmView)
	var list []string
	if json.Unmarshal([]byte(s), &list) == nil {
		return len(list) > 0, ""
	}
	var one string
	if json.Unmarshal([]byte(s), &one) == nil {
		return one != "", ""
	}
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Summary string `json:"summary"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(s), &e) == nil && e.Error.Code != "" && e.Error.Code != "E404" {
		return false, strings.TrimSpace(e.Error.Code + " " + e.Error.Summary)
	}
	return false, ""
}
