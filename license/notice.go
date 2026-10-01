package license

import (
	"fmt"
	"strings"
	"time"
)

// AppSlug is the Claudinite GitHub App's slug, which its install link
// names (ClaudiniteLicenses#1).
const AppSlug = "claudinite"

// InstallURL is where an account owner installs the Claudinite App.
const InstallURL = "https://github.com/apps/" + AppSlug + "/installations/new"

// offList is what a degraded session loses.
const offList = "work checks, forced skill loading and in-session growth are off"

// LinkFor is the link a cause wants in its notice, or "".
func LinkFor(c Cause) string {
	switch c {
	case CauseAppNotInstalled, "no-plan", "repo-not-visible":
		return InstallURL
	}
	return ""
}

func checkout(key *KeyPayload) string {
	if key != nil && key.CheckoutURL != nil {
		return *key.CheckoutURL
	}
	if key != nil && key.PortalURL != nil {
		return *key.PortalURL
	}
	return ""
}

func withLink(sentence, link string) string {
	if link == "" {
		return sentence + "."
	}
	return sentence + ": " + link + "."
}

// NoticeFor is the one sentence a session tells Claude to pass on to the
// person, for a held key's state or for the cause of holding none; ""
// when there is nothing to say (an ok key).
func NoticeFor(key *KeyPayload, cause Cause, detail, link string) string {
	if key != nil {
		owner := key.OwnerLogin
		switch {
		case key.State == "unverified":
			return "[cn] license unverified: the license server could not verify this session; every feature is on."
		case key.State == "grace":
			until := "soon"
			if key.GraceUntil != nil {
				until = time.Unix(*key.GraceUntil, 0).UTC().Format("2006-01-02")
			}
			over := ""
			if key.Seats != nil && key.Seats.Counted > key.Seats.Paid {
				over = fmt.Sprintf(" by %d", key.Seats.Counted-key.Seats.Paid)
			}
			return withLink(fmt.Sprintf("[cn] license grace: tell the person that %s's license is over its paid seats%s and every feature stays on until %s, after which someone who manages %s's plan must add seats", owner, over, until, owner), checkout(key))
		case key.State == "degraded" && len(key.Features) == 0:
			return withLink(fmt.Sprintf("[cn] license degraded: this session's seat was refused, so %s; tell the person that someone who manages %s's plan can add seats", offList, owner), checkout(key))
		case key.State == "degraded":
			return withLink(fmt.Sprintf("[cn] license degraded: %s's license is degraded, so the features it no longer lists are off; tell the person that someone who manages %s's plan can restore them", owner, owner), checkout(key))
		case key.Notice != nil && *key.Notice == "over-within-headroom":
			return withLink(fmt.Sprintf("[cn] license over its paid seats within the tolerance: tell the person that %s has more users than it pays for and someone who manages its plan should add seats before grace starts", owner), checkout(key))
		}
		return ""
	}
	if link == "" {
		link = LinkFor(cause)
	}
	switch cause {
	case "":
		return ""
	case CauseAppNotInstalled:
		return withLink(fmt.Sprintf("[cn] license degraded: no key came because the Claudinite GitHub App is not installed on this repo (or GitHub did not answer), so %s; tell the person to ask an owner of the account to install it", offList), link)
	case CauseNoPushAccess:
		return fmt.Sprintf("[cn] license degraded: GitHub refused the key request because the person lacks push access to this repo, so %s; tell the person.", offList)
	case CauseGitHubUnreachable:
		return fmt.Sprintf("[cn] license degraded: GitHub did not answer the key request, so %s; tell the person the next session asks again.", offList)
	case CauseServerUnreachable:
		return fmt.Sprintf("[cn] license degraded: the license server did not answer and no recent key is cached, so %s; tell the person the next session asks again.", offList)
	case CauseLoginExpired:
		return fmt.Sprintf("[cn] license degraded: this machine's Claudinite sign-in has expired or is missing, so %s; tell the person to run cn login.", offList)
	case CauseNoGitHubRemote:
		return fmt.Sprintf("[cn] license degraded: this repo's origin is not a GitHub repository, so it cannot request a key and %s; tell the person.", offList)
	case CauseGitHubUser:
		return fmt.Sprintf("[cn] license degraded: GitHub did not name a user for this session (a bot or a failed GET /user), so %s; tell the person.", offList)
	case CauseBindPlan, CauseBindRepo, CauseBindUser, CauseBindNonce:
		what := strings.TrimSpace(detail)
		if what == "" {
			what = string(cause)
		}
		if cause != CauseBindPlan {
			return fmt.Sprintf("[cn] license degraded: the key does not fit this session (%s), so %s; tell the person the next session asks again.", what, offList)
		}
		return withLink(fmt.Sprintf("[cn] license degraded: the key does not fit this session (%s), so %s; tell the person to check the plan the repo is on", what, offList), link)
	case CauseKeyRefused:
		return fmt.Sprintf("[cn] license degraded: the key did not verify (%s), so %s; tell the person.", detail, offList)
	case CauseStateFile:
		return fmt.Sprintf("[cn] license degraded: the session's license state file is unreadable, so %s; tell the person the next session asks again.", offList)
	case CauseActions:
		return "[cn] license: a GitHub Actions job is not a session and requests no session key."
	case "no-plan":
		return withLink(fmt.Sprintf("[cn] license degraded: the license server refused the key because this private repo has no plan (the Public plan covers public repos), so %s; tell the person an owner picks a plan where the App is installed", offList), link)
	}
	sentence := fmt.Sprintf("[cn] license degraded: the license server refused the key (%s", cause)
	if detail != "" {
		sentence += ": " + detail
	}
	return withLink(sentence+"), so "+offList+"; tell the person", link)
}
