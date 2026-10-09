package addpacks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsregex"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packindex"
)

// The force: a person named the packs, the repos and the configuration,
// and the sweep places a requested work list in each named member. It is
// refused whole, before any member is touched, on any ground below: a
// partly applied mass addition is the state hardest to see and to undo.

// Qualify is the owner/name a typed name means, lowercased.
func Qualify(name, owner string) string {
	if !strings.Contains(name, "/") {
		name = owner + "/" + name
	}
	return strings.ToLower(name)
}

// UnknownPacks are the ids the catalog does not offer: one reaching a
// declaration is a blocking settings error there.
func UnknownPacks(addPacks []string, packs []packindex.CatalogPack) []string {
	known := map[string]bool{}
	for _, p := range packs {
		known[p.ID] = true
	}
	out := []string{}
	for _, id := range addPacks {
		if !known[id] {
			out = append(out, id)
		}
	}
	return out
}

// Unanswered is one adoption question a force did not answer.
type Unanswered struct {
	Pack     string `json:"pack"`
	Question string `json:"question"`
	Prompt   string `json:"prompt"`
}

// UnansweredQuestions is every question the named packs ask that the
// answers leave blank. `n/a` is an answer; only an absent one refuses.
func UnansweredQuestions(addPacks []string, packs []packindex.CatalogPack, answers *Obj) []Unanswered {
	byID := map[string]packindex.CatalogPack{}
	for _, p := range packs {
		byID[p.ID] = p
	}
	out := []Unanswered{}
	for _, id := range addPacks {
		for _, q := range byID[id].Questions {
			if jsregex.Trim(answerOf(answers, id, q.ID)) == "" {
				out = append(out, Unanswered{id, q.ID, q.Prompt})
			}
		}
	}
	return out
}

func answerOf(answers *Obj, pack, question string) string {
	v, _ := answers.Get(pack)
	o, _ := v.(*Obj)
	a, _ := o.Get(question)
	switch t := a.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	return Stringify(a, "")
}

// EntryFor is the declaration entry a requested list asks the member to
// write for id.
func EntryFor(id string, config, answers *Obj) *Obj {
	e := NewObj()
	e.Set("id", id)
	if v, ok := config.Get(id); ok {
		if o, _ := v.(*Obj); o.Len() > 0 {
			e.Set("config", o)
		}
	}
	if v, ok := answers.Get(id); ok {
		if o, _ := v.(*Obj); o.Len() > 0 {
			e.Set("answers", o)
		}
	}
	return e
}

// belongsLine is a list item naming a pack and what it is for.
func belongsLine(id string, byID map[string]packindex.CatalogPack) string {
	if b := byID[id].Belongs; b != "" {
		return fmt.Sprintf("- **`%s`** — %s", id, b)
	}
	return fmt.Sprintf("- **`%s`**", id)
}

func byID(packs []packindex.CatalogPack) map[string]packindex.CatalogPack {
	m := map[string]packindex.CatalogPack{}
	for _, p := range packs {
		m[p.ID] = p
	}
	return m
}

// Request is what a force asks of one member.
type Request struct {
	AddPacks    []string
	PackConfig  *Obj
	PackAnswers *Obj
	Packs       []packindex.CatalogPack
	// Enforcer is the manager, "" to leave it unnamed; Settings is the
	// member's declaration file.
	Enforcer, Settings string
}

// RequestedBody is a requested list's body: the packs, and the entries to
// write rendered as the JSON they will become, which the member reads back
// with EntriesIn.
func RequestedBody(r Request) string {
	entries := []any{}
	for _, id := range r.AddPacks {
		entries = append(entries, EntryFor(id, r.PackConfig, r.PackAnswers))
	}
	via := ""
	if r.Enforcer != "" {
		via = fmt.Sprintf(" (via `%s`)", r.Enforcer)
	}
	lines := []string{
		"This repo is to declare the packs below. **Not a fingerprint's suspicion**: the fleet",
		"owner requested it by hand" + via + ", with the configuration and the adoption-interview",
		"answers already decided. Nothing has been written to this repo — the adoption is this",
		"repo's own adopt-requested-packs task's job, and its scheduler has been fired.",
		"",
		"## Packs to add",
		"",
	}
	ids := byID(r.Packs)
	for _, id := range r.AddPacks {
		lines = append(lines, belongsLine(id, ids))
	}
	lines = append(lines,
		"",
		"## The declaration entries to write",
		"",
		"Merge these verbatim into `"+r.Settings+"`'s `packs` (into an entry the repo",
		"already carries where one exists — never replacing a config it already chose):",
		"",
		"```json",
		Stringify(entries, "  "),
		"```",
		"",
		"The `answers` are the owner's answers to those packs' adoption interviews, recorded",
		"verbatim as adopt-pack requires. **Every** question these packs ask is answered above —",
		"the forced fleet run refuses to file this issue otherwise — so nothing here is left to",
		"guess.",
		"",
		"Converged by the fleet's add-missing-packs sweep: it closes this `completed` once the",
		"declaration carries the packs.",
	)
	return strings.Join(lines, "\n")
}

// Target is one member a force places a list in: the packs it lacks and
// its declaration file.
type Target struct {
	FullName      string   `json:"fullName"`
	Missing       []string `json:"missing"`
	DefaultBranch *string  `json:"defaultBranch"`
	Settings      string   `json:"-"`
}

// Resolved is a force's vetted targets and the named repos that already
// declare every named pack.
type Resolved struct {
	Targets         []Target `json:"targets"`
	AlreadyDeclared []string `json:"alreadyDeclared"`
}

// ResolveTargets reads and vets every named repo before anything is
// written; any problem refuses the whole force, and a read the grant
// refused is a grant error.
func ResolveTargets(gh fleet.GH, repos []string, owner string, addPacks []string, branches map[string]string) (Resolved, error) {
	out := Resolved{Targets: []Target{}, AlreadyDeclared: []string{}}
	var problems []string
	grant := false
	for _, name := range repos {
		full := Qualify(name, owner)
		if !strings.HasPrefix(full, owner+"/") {
			problems = append(problems, fmt.Sprintf("%s is not under the configured owner `%s` — this fleet reaches no other", full, owner))
			continue
		}
		m, err := fleet.ReadMember(gh, full, branches[full])
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: could not read its declaration (%s)", full, err.Error()))
			grant = grant || fleet.IsGrant(err)
			continue
		}
		if !m.Covered() {
			problems = append(problems, fmt.Sprintf("%s is not a covered member (no .claudinite/settings.* and no %s) — adoption of the whole corpus is the census's business, not this task's", full, fleet.NodeSettingsFile))
			continue
		}
		if m.Dormant {
			problems = append(problems, fmt.Sprintf("%s declared itself dormant — it stopped its own upkeep on purpose, so adding a pack to it is work it has opted out of", full))
			continue
		}
		declared := DeclaredIDs(m)
		missing := []string{}
		for _, id := range addPacks {
			if !contains(declared, id) {
				missing = append(missing, id)
			}
		}
		if len(missing) == 0 {
			out.AlreadyDeclared = append(out.AlreadyDeclared, full)
			continue
		}
		t := Target{FullName: full, Missing: missing, Settings: m.SettingsPath()}
		if b, ok := branches[full]; ok {
			t.DefaultBranch = &b
		}
		out.Targets = append(out.Targets, t)
	}
	if len(problems) > 0 {
		msg := fmt.Sprintf("%d named repo(s) cannot be forced, so none were: %s", len(problems), strings.Join(problems, "; "))
		if grant {
			return Resolved{}, &fleet.GrantError{Msg: msg}
		}
		return Resolved{}, &Refusal{msg}
	}
	return out, nil
}

// DeclaredIDs is every id a member's declaration names, as written.
func DeclaredIDs(m fleet.Member) []string {
	var out []string
	switch m.Shape {
	case fleet.ShapeCn:
		for _, e := range m.Packs.Entries {
			out = append(out, e.ID)
		}
	case fleet.ShapeNode:
		cfg, _ := m.Node.(map[string]any)
		entries, _ := cfg["packs"].([]any)
		out = NodeDeclared(entries)
	}
	return out
}

// NodeDeclared is a Node declaration's packs as ids, entries naming none
// dropped.
func NodeDeclared(entries []any) []string {
	var out []string
	for _, e := range entries {
		switch t := e.(type) {
		case string:
			if t != "" {
				out = append(out, t)
			}
		case map[string]any:
			if id, ok := t["id"].(string); ok && id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// ConvergeRequested places or rewrites the requested list in one member,
// returning its number and the action taken, "" when it was already
// right.
func ConvergeRequested(gh fleet.GH, repo, body string) (int, string, error) {
	open, _, err := fleet.TitledIssues(gh, repo, IsWorkListTitle)
	if err != nil {
		return 0, "", err
	}
	for _, i := range open {
		if i.Title == RequestedTitle {
			written, err := Remark(gh, repo, i, MarkedBody(body, &i, 0))
			if err != nil || !written {
				return i.Number, "", err
			}
			return i.Number, fmt.Sprintf("updated #%d", i.Number), nil
		}
	}
	if err := EnsureMark(gh, repo); err != nil {
		return 0, "", err
	}
	n, err := openIssue(gh, repo, RequestedTitle, MarkedBody(body, nil, OtherOpenWorkList(open, RequestedTitle)), "requested-packs")
	if err != nil {
		return 0, "", err
	}
	return n, fmt.Sprintf("opened #%d", n), nil
}

// openIssue opens a marked work list.
func openIssue(gh fleet.GH, repo, title, body, what string) (int, error) {
	r, err := gh("POST", "/repos/"+repo+"/issues", map[string]any{"title": title, "body": body, "labels": []string{Mark}})
	if err != nil {
		return 0, err
	}
	var created struct {
		Number int `json:"number"`
	}
	if r.Status != 201 || json.Unmarshal(r.JSON, &created) != nil {
		msg := fmt.Sprintf("creating the %s issue in %s returned %d", what, repo, r.Status)
		if r.Status == 403 {
			return 0, &fleet.GrantError{Msg: msg + fleet.ForbiddenHint("/repos/"+repo+"/issues")}
		}
		return 0, errors.New(msg)
	}
	return created.Number, nil
}

// CloseSatisfied closes a member's requested list once its declaration
// carries every pack the list asked for; "" when there is nothing to
// close, the list is still outstanding or its block is unreadable (left
// for a human, never closed blind).
func CloseSatisfied(gh fleet.GH, repo string, declared []string, open []fleet.Issue) (string, error) {
	for _, i := range open {
		if i.Title != RequestedTitle {
			continue
		}
		entries := EntriesIn(i.Body)
		if entries == nil {
			return "", nil
		}
		for _, id := range EntryIDs(entries) {
			if !contains(declared, id) {
				return "", nil
			}
		}
		if _, err := fleet.Expect(gh, "POST", fmt.Sprintf("/repos/%s/issues/%d/comments", repo, i.Number),
			map[string]string{"body": "Closed by the fleet's add-missing-packs sweep: the declaration now carries every requested pack."}, 201); err != nil {
			return "", err
		}
		if _, err := fleet.Expect(gh, "PATCH", fmt.Sprintf("/repos/%s/issues/%d", repo, i.Number),
			map[string]string{"state": "closed", "state_reason": "completed"}, 200); err != nil {
			return "", err
		}
		return fmt.Sprintf("closed #%d (%s: request satisfied)", i.Number, repo), nil
	}
	return "", nil
}

// ForceSummary is the force's own section of the run summary.
func ForceSummary(owner string, addPacks []string, targets []Target, already, actions, fired []string) string {
	quoted := make([]string, len(addPacks))
	for i, p := range addPacks {
		quoted[i] = "`" + p + "`"
	}
	adopt := "**To adopt:** none — every named repo already declares every named pack."
	if len(targets) > 0 {
		var rows []string
		for _, t := range targets {
			rows = append(rows, fmt.Sprintf("- `%s` → %s", t.FullName, strings.Join(t.Missing, ", ")))
		}
		adopt = "**To adopt (issue in each repo, scheduler fired):**\n" + strings.Join(rows, "\n")
	}
	lines := []string{
		"# Forced pack addition — " + owner,
		"Packs: " + strings.Join(quoted, ", "),
		"| requested | work lists placed | schedulers fired | already declaring |",
		"| --- | --- | --- | --- |",
		fmt.Sprintf("| %d | %d | %d | %d |", len(targets)+len(already), len(targets), len(fired), len(already)),
		adopt,
	}
	if len(already) > 0 {
		lines = append(lines, "**Already declaring (untouched):** "+strings.Join(already, ", "))
	}
	if len(actions) > 0 {
		lines = append(lines, "**Issue actions:** "+strings.Join(actions, "; "))
	}
	lines = append(lines,
		"_Nothing was written to any member's tree. Each member's own adopt-requested-packs_",
		"_task adopts what its issue says — with the repo checked out, one reviewed PR there._")
	return strings.Join(lines, "\n")
}
