package roster

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
)

// The adoption issue family's label, as Node's.
const (
	Label            = "fleet-adoption"
	LabelColor       = "1D76DB"
	LabelDescription = "Repo awaiting adoption into the Claudinite fleet"
)

var titleRE = regexp.MustCompile(`^Adopt (\S+/\S+) into the Claudinite fleet$`)

// Title is the one adoption issue's title for repo.
func Title(repo string) string { return "Adopt " + repo + " into the Claudinite fleet" }

// Body is the adoption issue's body: cn init and the adopt-claudinite
// skill, where Node named bootstrap.md.
func Body(repo string) string {
	return strings.Join([]string{
		"`" + repo + "` exists under this account but does not mount Claudinite (no tracked",
		"`.claudinite/settings.*` on its default branch) and is not on the exclude list.",
		"",
		"Pick one:",
		"",
		"- **Adopt it** — run the adoption in a session on that repo (ask for \"adopt Claudinite\";",
		"  the `adopt-claudinite` skill runs `cn init`). This is a human-initiated step by",
		"  design: adoption is the one thing a repo cannot do for itself, because the scheduler",
		"  that would run it is what adoption installs. Nothing will do it on your behalf.",
		"- **Keep it out** — add `" + repo + "` to the claudinite-fleet-sheepdog pack entry's `config.exclude` in this",
		"  (claudinite-fleet-sheepdog) repo's `.claudinite/settings.*`, with a reason.",
		"",
		"This issue is converged by the daily fleet-roster task: it closes itself once the",
		"repo is covered (`completed`) or opted out (`not planned`), and a close without either",
		"gets reopened while the repo stays uncovered.",
	}, "\n")
}

// ConvergeAdoption opens an issue per uncovered repository, reopens one a
// person closed completed while it stays uncovered, honours a not_planned
// close, and closes an open one once its repository is covered (completed)
// or ignored or gone (not_planned). It returns the actions taken.
func ConvergeAdoption(gh fleet.GH, home string, uncovered, covered, ignored []string) ([]string, error) {
	actions := []string{}
	open, closed, err := fleet.LabeledIssues(gh, home, Label)
	if err != nil {
		return actions, err
	}
	byTitle := map[string]fleet.Issue{}
	var order []string
	for _, i := range open {
		if _, ok := byTitle[i.Title]; !ok {
			order = append(order, i.Title)
		}
		byTitle[i.Title] = i
	}
	for _, repo := range uncovered {
		title := Title(repo)
		if _, ok := byTitle[title]; ok {
			continue
		}
		var prior *fleet.Issue
		for i := range closed {
			c := closed[i]
			if c.Title != title {
				continue
			}
			if prior == nil || closedAt(c) > closedAt(*prior) {
				prior = &closed[i]
			}
		}
		if prior != nil && prior.StateReason != nil && *prior.StateReason == "not_planned" {
			continue
		}
		if prior != nil {
			if _, err := fleet.Expect(gh, "PATCH", fmt.Sprintf("/repos/%s/issues/%d", home, prior.Number), map[string]any{"state": "open"}, 200); err != nil {
				return actions, err
			}
			if _, err := fleet.Expect(gh, "POST", fmt.Sprintf("/repos/%s/issues/%d/comments", home, prior.Number),
				map[string]any{"body": "Reopened by the roster sweep: `" + repo + "` is still uncovered."}, 201); err != nil {
				return actions, err
			}
			actions = append(actions, fmt.Sprintf("reopened #%d (%s)", prior.Number, repo))
			continue
		}
		r, err := gh("POST", "/repos/"+home+"/issues", map[string]any{"title": title, "body": Body(repo), "labels": []string{Label}})
		if err != nil {
			return actions, err
		}
		var made struct {
			Number int `json:"number"`
		}
		if r.Status != 201 || jsonInto(r.JSON, &made) != nil {
			return actions, fmt.Errorf("creating adoption issue for %s returned %d", repo, r.Status)
		}
		actions = append(actions, fmt.Sprintf("opened #%d (%s)", made.Number, repo))
	}
	has := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	for _, title := range order {
		issue := byTitle[title]
		m := titleRE.FindStringSubmatch(title)
		if m == nil {
			continue
		}
		repo := strings.ToLower(m[1])
		var why, note string
		switch {
		case has(covered, repo):
			why, note = "completed", "now mounts Claudinite — covered"
		case has(ignored, repo):
			why, note = "not_planned", "ignored (the claudinite-fleet-sheepdog pack entry's config.exclude)"
		case !has(uncovered, repo):
			why, note = "not_planned", "no longer an adoption candidate (deleted, archived, transferred, or now a fork)"
		default:
			continue
		}
		if _, err := fleet.Expect(gh, "POST", fmt.Sprintf("/repos/%s/issues/%d/comments", home, issue.Number),
			map[string]any{"body": "Closed by the roster sweep: `" + m[1] + "` " + note + "."}, 201); err != nil {
			return actions, err
		}
		if _, err := fleet.Expect(gh, "PATCH", fmt.Sprintf("/repos/%s/issues/%d", home, issue.Number), map[string]any{"state": "closed", "state_reason": why}, 200); err != nil {
			return actions, err
		}
		actions = append(actions, fmt.Sprintf("closed #%d (%s: %s)", issue.Number, m[1], note))
	}
	return actions, nil
}

func closedAt(i fleet.Issue) string {
	if i.ClosedAt == nil {
		return ""
	}
	return *i.ClosedAt
}

func jsonInto(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }
