package decide

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/addpacks"
	"github.com/missingbulb/ClaudiniteEngine/fleet/seeds"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// nodePack is a pack as a fixture spells it: the Node manifest's fields,
// its fingerprint as data.
type nodePack struct {
	ID                  string                   `json:"id"`
	RuleRoutingGuidance struct{ Belongs string } `json:"ruleRoutingGuidance"`
	Questions           []packindex.Question     `json:"questions"`
	RelevanceDetector   json.RawMessage          `json:"relevanceDetector"`
	Local               bool                     `json:"local"`
}

// catalogPacks reads a fixture's packs as catalog entries; a local pack
// is no catalog's, since a repo declares its own by hand.
func catalogPacks(in []nodePack) ([]packindex.CatalogPack, error) {
	out := []packindex.CatalogPack{}
	for _, p := range in {
		if p.Local {
			continue
		}
		d, err := packindex.DecodeDetector(p.RelevanceDetector)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", p.ID, err)
		}
		out = append(out, packindex.CatalogPack{ID: p.ID, Channel: packindex.Stable, Belongs: p.RuleRoutingGuidance.Belongs, Questions: p.Questions, RelevanceDetector: d})
	}
	return out, nil
}

// obj is a fixture value read with its key order kept, an empty *Obj for
// none.
func obj(raw json.RawMessage) *addpacks.Obj {
	if len(raw) == 0 || string(raw) == "null" {
		return addpacks.NewObj()
	}
	v, err := addpacks.DecodeObj(raw)
	if o, ok := v.(*addpacks.Obj); err == nil && ok {
		return o
	}
	return addpacks.NewObj()
}

// withCalls is a core's answer beside the calls it made, or its error.
func withCalls(result any, err error, calls *[][]any) any {
	if err != nil {
		return map[string]any{"error": err.Error(), "calls": *calls}
	}
	return map[string]any{"result": result, "calls": *calls}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func params(raw []byte) (any, error) {
	var in struct {
		Cases []struct {
			Argv   []string
			Params map[string]string
		}
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := []any{}
	for _, c := range in.Cases {
		p, err := addpacks.ParseParams(c.Argv, c.Params)
		if err != nil {
			out = append(out, map[string]string{"error": err.Error()})
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func force(raw []byte) (any, error) {
	var in struct {
		Fn, FullName, Body, Owner string
		Packs                     []nodePack
		Calls                     map[string]json.RawMessage
		Cases                     json.RawMessage
		AddPacks, Repos           []string
		PackAnswers               json.RawMessage
		Args                      json.RawMessage
		ReposByName               map[string]struct {
			DefaultBranch string `json:"default_branch"`
		}
		Decl struct{ Packs []any }
		Open []fleet.Issue
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	packs, err := catalogPacks(in.Packs)
	if err != nil {
		return nil, err
	}
	gh, calls := fake(in.Calls)
	switch in.Fn {
	case "qualify":
		var cases [][2]string
		if err := json.Unmarshal(in.Cases, &cases); err != nil {
			return nil, err
		}
		out := []string{}
		for _, c := range cases {
			out = append(out, addpacks.Qualify(c[0], c[1]))
		}
		return out, nil
	case "unknownPacks":
		return addpacks.UnknownPacks(in.AddPacks, packs), nil
	case "unansweredQuestions":
		return addpacks.UnansweredQuestions(in.AddPacks, packs, obj(in.PackAnswers)), nil
	case "entryFor":
		var cases []struct {
			ID          string
			PackConfig  json.RawMessage
			PackAnswers json.RawMessage
		}
		if err := json.Unmarshal(in.Cases, &cases); err != nil {
			return nil, err
		}
		out := []any{}
		for _, c := range cases {
			out = append(out, addpacks.EntryFor(c.ID, obj(c.PackConfig), obj(c.PackAnswers)))
		}
		return out, nil
	case "requestedBody":
		var a struct {
			AddPacks    []string
			PackConfig  json.RawMessage
			PackAnswers json.RawMessage
			Enforcer    string
		}
		if err := json.Unmarshal(in.Args, &a); err != nil {
			return nil, err
		}
		return addpacks.RequestedBody(addpacks.Request{AddPacks: a.AddPacks, PackConfig: obj(a.PackConfig), PackAnswers: obj(a.PackAnswers),
			Packs: packs, Enforcer: a.Enforcer, Settings: fleet.NodeSettingsFile}), nil
	case "forceSummary":
		var a struct {
			Owner                     string
			AddPacks, AlreadyDeclared []string
			Actions, Fired            []string
			Targets                   []addpacks.Target
		}
		if err := json.Unmarshal(in.Args, &a); err != nil {
			return nil, err
		}
		return addpacks.ForceSummary(a.Owner, a.AddPacks, a.Targets, a.AlreadyDeclared, a.Actions, a.Fired), nil
	case "resolveTargets":
		branches := map[string]string{}
		for k, v := range in.ReposByName {
			branches[k] = v.DefaultBranch
		}
		res, err := addpacks.ResolveTargets(gh, in.Repos, in.Owner, in.AddPacks, branches)
		return withCalls(res, err, calls), nil
	case "convergeRequested":
		n, action, err := addpacks.ConvergeRequested(gh, in.FullName, in.Body)
		return withCalls(map[string]any{"number": n, "action": nullable(action)}, err, calls), nil
	case "closeSatisfied":
		closed, err := addpacks.CloseSatisfied(gh, in.FullName, addpacks.NodeDeclared(in.Decl.Packs), in.Open)
		return withCalls(nullable(closed), err, calls), nil
	}
	return nil, fmt.Errorf("unknown force fn %q", in.Fn)
}

func protocol(raw []byte) (any, error) {
	var in struct {
		Bodies    []string
		Targeting []struct {
			Body      string
			BlockedBy int
		}
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	entries := []any{}
	for _, b := range in.Bodies {
		if e := addpacks.EntriesIn(b); e != nil {
			entries = append(entries, e)
		} else {
			entries = append(entries, nil)
		}
	}
	targeted := []string{}
	for _, t := range in.Targeting {
		targeted = append(targeted, addpacks.WithTargeting(t.Body, t.BlockedBy))
	}
	return map[string]any{"constants": addpacks.Protocol(), "entriesIn": entries, "withTargeting": targeted}, nil
}

func mark(raw []byte) (any, error) {
	var in struct {
		Fn, FullName, Body string
		Existing           fleet.Issue
		Calls              map[string]json.RawMessage
		Cases              json.RawMessage
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	gh, calls := fake(in.Calls)
	switch in.Fn {
	case "markedBody":
		var cases []struct {
			Body      string
			Existing  *fleet.Issue
			BlockedBy int
		}
		if err := json.Unmarshal(in.Cases, &cases); err != nil {
			return nil, err
		}
		out := []string{}
		for _, c := range cases {
			out = append(out, addpacks.MarkedBody(c.Body, c.Existing, c.BlockedBy))
		}
		return out, nil
	case "otherOpenWorkList":
		var cases []struct {
			Open  []fleet.Issue
			Title string
		}
		if err := json.Unmarshal(in.Cases, &cases); err != nil {
			return nil, err
		}
		out := []any{}
		for _, c := range cases {
			if n := addpacks.OtherOpenWorkList(c.Open, c.Title); n != 0 {
				out = append(out, n)
			} else {
				out = append(out, nil)
			}
		}
		return out, nil
	case "remark":
		written, err := addpacks.Remark(gh, in.FullName, in.Existing, in.Body)
		if err != nil {
			return map[string]any{"error": err.Error(), "calls": *calls}, nil
		}
		return map[string]any{"written": written, "calls": *calls}, nil
	}
	return nil, fmt.Errorf("unknown mark fn %q", in.Fn)
}

func scan(raw []byte) (any, error) {
	var in struct {
		Fn, FullName, Body, Home, Owner string
		Fits                            []string
		Undecided                       []addpacks.Undecided
		Packs                           []nodePack
		Exclude                         []string
		Repos                           []string
		Calls                           map[string]json.RawMessage
		Args                            json.RawMessage
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	packs, err := catalogPacks(in.Packs)
	if err != nil {
		return nil, err
	}
	gh, calls := fake(in.Calls)
	switch in.Fn {
	case "suspectedBody":
		return addpacks.SuspectedBody(in.Fits, in.Undecided, fleet.NodeSettingsFile, packs), nil
	case "fitSummary":
		var a struct {
			Owner, Home string
			PackCount   int
			addpacks.Scan
			Fired      []string
			RepoFilter []string
		}
		if err := json.Unmarshal(in.Args, &a); err != nil {
			return nil, err
		}
		return addpacks.FitSummary(a.Owner, a.Home, a.PackCount, a.Scan, a.Fired, a.RepoFilter), nil
	case "convergeSuspected":
		c, err := addpacks.ConvergeSuspected(gh, in.FullName, in.Fits, in.Body)
		return withCalls(c, err, calls), nil
	case "runScan":
		cfg := fleet.Config{Owner: strings.ToLower(in.Owner)}
		for _, e := range in.Exclude {
			cfg.Exclude = append(cfg.Exclude, strings.ToLower(e))
		}
		repos, err := addpacks.Enumerate(gh, cfg.Owner)
		if err != nil {
			return map[string]any{"error": err.Error(), "calls": *calls}, nil
		}
		var scoped []string
		for _, r := range in.Repos {
			scoped = append(scoped, strings.ToLower(r))
		}
		s := addpacks.RunScan(gh, repos, in.Home, cfg, addpacks.FixedCorpus(packs), scoped)
		return map[string]any{"findings": s.Findings, "unknown": s.Unknown, "toFire": s.ToFire, "actions": s.Actions,
			"fitted": s.Fitted, "dormant": s.Dormant, "outOfScope": s.OutOfScope, "calls": *calls}, nil
	}
	return nil, fmt.Errorf("unknown scan fn %q", in.Fn)
}

func fitCore(raw []byte) (any, error) {
	var in struct {
		Fn, Repo, Ref string
		Packs         []nodePack
		Declared      []any
		Answers       map[string]json.RawMessage
		Tracked       []string
		Truncated     bool
		Budget        int
		Calls         map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	packs, err := catalogPacks(in.Packs)
	if err != nil {
		return nil, err
	}
	var declared []string
	for _, e := range in.Declared {
		if id := addpacks.DeclaredID(e); id != "" {
			declared = append(declared, id)
		}
	}
	gh, calls := fake(in.Calls)
	switch in.Fn {
	case "fitCandidates":
		out := []string{}
		for _, p := range addpacks.Candidates(packs, declared) {
			out = append(out, p.ID)
		}
		return out, nil
	case "undeclaredFits":
		return addpacks.UndeclaredFits(packs, declared, func(p packindex.CatalogPack) (addpacks.Verdict, error) {
			a := in.Answers[p.ID]
			var s string
			if json.Unmarshal(a, &s) == nil && strings.HasPrefix(s, "throw:") {
				return addpacks.Verdict{}, fmt.Errorf("%s", strings.TrimPrefix(s, "throw:"))
			}
			var b bool
			if string(a) == "null" || len(a) == 0 {
				return addpacks.Verdict{}, nil
			}
			if json.Unmarshal(a, &b) == nil {
				return addpacks.Verdict{Fit: &b}, nil
			}
			var v addpacks.Verdict
			_ = json.Unmarshal(a, &v)
			return v, nil
		}), nil
	case "remote":
		evaluate := addpacks.RemoteEvaluator(gh, in.Repo, in.Ref, addpacks.Tree{Tracked: in.Tracked, Truncated: in.Truncated}, in.Budget)
		verdicts := map[string]any{}
		for _, p := range packs {
			v, err := evaluate(p)
			if err != nil {
				return nil, err
			}
			verdicts[p.ID] = v
		}
		return map[string]any{"verdicts": verdicts, "calls": *calls}, nil
	case "tree":
		t, err := addpacks.FetchTree(gh, in.Repo, in.Ref)
		return withCalls(t, err, calls), nil
	}
	return nil, fmt.Errorf("unknown fit fn %q", in.Fn)
}

func seedsCore(raw []byte) (any, error) {
	var in struct {
		Fn    string
		Cases []struct {
			Config   struct{ Packs []any }
			Seed     fleet.Seed
			Vendored bool
		}
		Seeds []fleet.Seed
		Cn    struct{ Format, Text string }
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	switch in.Fn {
	case "classify":
		out := []seeds.Verdict{}
		for _, c := range in.Cases {
			declared, hasConfig := false, false
			for _, e := range c.Config.Packs {
				if id, ok := e.(string); ok && id == c.Seed.ID {
					declared = true
					break
				}
				if o, ok := e.(map[string]any); ok && o["id"] == c.Seed.ID {
					_, hasConfig = o["config"]
					declared = true
					break
				}
			}
			out = append(out, seeds.Classify(declared, hasConfig, c.Seed, c.Vendored))
		}
		return out, nil
	case "withSeeds":
		text, err := seeds.Splice(in.Cn.Text, settings.Format(in.Cn.Format), in.Seeds)
		if err != nil {
			return map[string]string{"error": err.Error()}, nil
		}
		return text, nil
	}
	return nil, fmt.Errorf("unknown seeds fn %q", in.Fn)
}
