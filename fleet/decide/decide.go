// Package decide answers the fleet's pure cores over a parity fixture's
// JSON world: `cn fleet decide <core> --world FILE`, the parity harness's
// fleet face, never a run. Where a core reads GitHub, the world's calls
// table stands in for the API, as the Node shim's fake gh does.
package decide

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/roster"
	"github.com/missingbulb/ClaudiniteEngine/fleet/update"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Cores are the decisions Decide answers.
var Cores = []string{"token", "config", "dormancy", "dispatch", "scope", "freshness", "views", "reports", "adoption", "follow", "bag", "signal", "params", "force", "protocol", "mark", "fit", "scan", "seeds"}

// Decide answers one core over raw.
func Decide(core string, raw []byte) (any, error) {
	switch core {
	case "token":
		return token(raw)
	case "config":
		return config(raw)
	case "dormancy":
		var in struct{ Configs []any }
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		out := []bool{}
		for _, c := range in.Configs {
			out = append(out, fleet.IsDormant(c))
		}
		return out, nil
	case "dispatch":
		var in struct{ Statuses []int }
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		out := []fleet.Dispatch{}
		for _, s := range in.Statuses {
			out = append(out, fleet.ClassifyDispatch(s))
		}
		return out, nil
	case "scope":
		return scope(raw)
	case "freshness":
		var in nodeFresh
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return fleet.Classify(in.adapt()), nil
	case "views":
		var in struct{ Roster []nodeEntry }
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		r := entries(in.Roster)
		return map[string]any{"coverage": roster.CoverageView(r), "freshness": roster.FreshnessView(r)}, nil
	case "reports":
		return reports(raw)
	case "adoption":
		return adoption(raw)
	case "follow":
		return follow(raw)
	case "bag":
		var in struct{ Raw string }
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return fleet.ParseParamBag(in.Raw), nil
	case "signal":
		var in struct {
			Owner, SinceIso string
			Calls           map[string]json.RawMessage
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		gh, _ := fake(in.Calls)
		return fleet.ReadFleet(gh, in.Owner, in.SinceIso), nil
	case "params":
		return params(raw)
	case "force":
		return force(raw)
	case "protocol":
		return protocol(raw)
	case "mark":
		return mark(raw)
	case "fit":
		return fitCore(raw)
	case "scan":
		return scan(raw)
	case "seeds":
		return seedsCore(raw)
	}
	return nil, fmt.Errorf("unknown fleet core %q (want one of %s)", core, strings.Join(Cores, ", "))
}

func token(raw []byte) (any, error) {
	var in struct {
		Sweeps, Paths []string
		Missing       []struct{ Sweep, Detail string }
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	grantFor, perm, hint := map[string]string{}, map[string]any{}, map[string]string{}
	for _, s := range in.Sweeps {
		grantFor[s] = fleet.GrantFor(s)
	}
	for _, p := range in.Paths {
		perm[p] = nil
		if pm := fleet.PermissionFor(p); pm != nil {
			perm[p] = pm.Permission
		}
		hint[p] = fleet.ForbiddenHint(p)
	}
	missing := []string{}
	for _, m := range in.Missing {
		missing = append(missing, fleet.MissingTokenError(m.Sweep, m.Detail))
	}
	return map[string]any{"grant": fleet.Grant(), "grantFor": grantFor, "permissionFor": perm, "hint": hint, "missing": missing, "handover": fleet.HandoverStep()}, nil
}

func config(raw []byte) (any, error) {
	var in struct {
		Cfg  map[string]any
		Home string
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	var entry any
	present := false
	packs, _ := in.Cfg["packs"].([]any)
	for _, p := range packs {
		if o, ok := p.(map[string]any); ok && o["id"] == fleet.PackID {
			entry, present = o["config"]
			break
		}
	}
	c, err := fleet.ParseConfig(entry, present, in.Home)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	seeds := []map[string]any{}
	for _, s := range c.PackSeeds {
		o := map[string]any{"id": s.ID}
		if s.Config != nil {
			o["config"] = s.Config
		}
		seeds = append(seeds, o)
	}
	out := map[string]any{"owner": c.Owner, "exclude": nonNil(c.Exclude), "packSeeds": seeds}
	if c.CanonRepoNamed {
		out["note"] = fleet.CanonRepoNote
	}
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func scope(raw []byte) (any, error) {
	var in struct {
		Owner   string
		Exclude []string
		Repos   []fleet.Repo
		Filter  *string
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	var cfg fleet.Config
	for _, e := range in.Exclude {
		cfg.Exclude = append(cfg.Exclude, strings.ToLower(e))
	}
	var filter update.Filter
	if in.Filter != nil {
		filter = update.ParseRepoFilter(*in.Filter, in.Owner)
	}
	scopes := []any{}
	for _, r := range in.Repos {
		if s := update.ClassifyScope(r, cfg, filter); s != nil {
			scopes = append(scopes, s)
		} else {
			scopes = append(scopes, nil)
		}
	}
	var f any
	if filter != nil {
		f = []string(filter)
	}
	return map[string]any{"filter": f, "scopes": scopes}, nil
}

// nodeFresh is the Node classifier's input: the member's stamped numbers
// and canon's.
type nodeFresh struct {
	HasScheduler bool
	Installed    nodeVersions
	Canon        nodeVersions
}

type nodeVersions struct {
	EngineVersion *string
	PackVersions  map[string]string
}

// engine3 reads a Node engine version <day>.<n> as cn's <day>.<n>.0.
func engine3(v *string) string {
	if v == nil {
		return ""
	}
	if strings.Count(*v, ".") == 1 {
		return *v + ".0"
	}
	return *v
}

// adapt is the shelf the canon numbers stand for: a packument offering
// canon's engine and an index offering each canon pack version, so the
// classification runs the real Candidate and Select and only the shelf's
// source differs.
func (n nodeFresh) adapt() fleet.FreshIn {
	in := fleet.FreshIn{Shape: fleet.ShapeCn, Format: settings.YAML, HasScheduler: n.HasScheduler,
		Pin: engine3(n.Installed.EngineVersion), Held: map[string]string{}, PackNext: map[string]string{}}
	for id, v := range n.Installed.PackVersions {
		in.Held[id] = v
	}
	if in.Pin != "" && n.Canon.EngineVersion != nil {
		p := &npmreg.Packument{Versions: map[string]npmreg.Version{}}
		c := engine3(n.Canon.EngineVersion)
		p.Versions[c] = npmreg.Version{Version: c}
		in.EngineNext = npmreg.Candidate(in.Pin, p, npmreg.StatesFromPackument(p)).Version
	}
	for id, held := range in.Held {
		there, ok := n.Canon.PackVersions[id]
		if !ok {
			continue
		}
		ix := packindex.Index{Pack: id, Versions: []packindex.Entry{{Version: there, Channel: "stable", MinEngineVersion: "60000.1"}}}
		if c := packindex.Select(ix, packindex.Want{Channel: "stable", Engine: in.Pin, Held: held}); c.Entry != nil {
			in.PackNext[id] = c.Entry.Version
		}
	}
	return in
}

// nodeEntry is a roster entry as the Node walk builds it.
type nodeEntry struct {
	roster.Entry
	Declaration json.RawMessage `json:"declaration"`
}

func entries(in []nodeEntry) []roster.Entry {
	out := []roster.Entry{}
	for _, e := range in {
		r := e.Entry
		r.Covered = len(e.Declaration) > 0 && string(e.Declaration) != "null"
		out = append(out, r)
	}
	return out
}

func reports(raw []byte) (any, error) {
	var in struct {
		Kind string
		Args json.RawMessage
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	var a struct {
		Owner, Home string
		roster.Coverage
		Actions    []string
		Fresh      []roster.Fresh
		Unhealthy  []roster.Unhealthy
		OutOfScope []string
		DryRun     bool
		Filter     []string
		Fired      []update.Row
		Followed   []struct {
			FullName, Outcome, Detail string
		}
		Failed  []update.Row
		Skipped json.RawMessage `json:"skipped"`
	}
	if err := json.Unmarshal(in.Args, &a); err != nil {
		return nil, err
	}
	switch in.Kind {
	case "coverage":
		if len(a.Skipped) > 0 {
			if err := json.Unmarshal(a.Skipped, &a.Coverage.Skipped); err != nil {
				return nil, err
			}
		}
		return roster.RenderCoverage(a.Owner, a.Home, a.Coverage, a.Actions), nil
	case "freshness":
		return roster.RenderFreshness(a.Owner, a.Home, roster.Freshness{Fresh: a.Fresh, Unhealthy: a.Unhealthy,
			Dormant: a.Dormant, Ignored: a.Ignored, OutOfScope: a.OutOfScope, Unknown: a.Unknown}), nil
	case "update", "verdict":
		r := update.Report{Owner: a.Owner, DryRun: a.DryRun, Failed: a.Failed}
		if a.Filter != nil {
			r.Filter = update.Filter(a.Filter)
		}
		for _, f := range a.Fired {
			r.Fired = append(r.Fired, update.Fired{Row: f})
		}
		for _, f := range a.Followed {
			r.Followed = append(r.Followed, update.Followed{Fired: update.Fired{Row: update.Row{FullName: f.FullName}}, Outcome: f.Outcome, Detail: f.Detail})
		}
		if len(a.Skipped) > 0 {
			_ = json.Unmarshal(a.Skipped, &r.Skipped)
		}
		if in.Kind == "update" {
			return r.Render(), nil
		}
		if v := r.Verdict(); v != "" {
			return v, nil
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown report %q", in.Kind)
}

func adoption(raw []byte) (any, error) {
	var in struct {
		Home                        string
		Uncovered, Covered, Ignored []string
		Calls                       map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	gh, calls := fake(in.Calls)
	actions, err := roster.ConvergeAdoption(gh, in.Home, nonNil(in.Uncovered), in.Covered, in.Ignored)
	if err != nil {
		return map[string]any{"error": err.Error(), "calls": *calls}, nil
	}
	return map[string]any{"actions": actions, "calls": *calls}, nil
}

func follow(raw []byte) (any, error) {
	var in struct {
		CanonEngine string
		BudgetMs    int64
		Members     []struct {
			FullName, FiredAt string
			WasFresh          bool
		}
		Script map[string][]struct {
			Engine *string
			Error  string
			Gone   bool
		}
		Runs map[string]any
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	var fired []update.Fired
	for _, m := range in.Members {
		fired = append(fired, update.Fired{Row: update.Row{FullName: m.FullName}, FiredAt: m.FiredAt, WasFresh: m.WasFresh})
	}
	polls := map[string]int{}
	canon := in.CanonEngine
	read := func(f update.Fired) (fleet.Freshness, error) {
		script := in.Script[f.FullName]
		i := min(polls[f.FullName], len(script)-1)
		polls[f.FullName]++
		step := script[i]
		switch {
		case step.Error != "":
			return fleet.Freshness{}, fmt.Errorf("%s", step.Error)
		case step.Gone:
			return fleet.Freshness{State: "no-declaration", Detail: "the settings file disappeared mid-run"}, nil
		}
		return fleet.Classify(nodeFresh{HasScheduler: true, Installed: nodeVersions{EngineVersion: step.Engine}, Canon: nodeVersions{EngineVersion: &canon}}.adapt()), nil
	}
	started := func(f update.Fired) (bool, error) {
		switch v := in.Runs[f.FullName].(type) {
		case bool:
			return v, nil
		case string:
			return false, fmt.Errorf("could not list %s runs: 500", fleet.Scheduler)
		}
		return false, nil
	}
	var clock int64
	sleeps, logs := []int64{}, []string{}
	out := update.Follow(fired, read, started, in.BudgetMs, update.Clock{
		Now:   func() int64 { return clock },
		Sleep: func(ms int64) { sleeps = append(sleeps, ms); clock += ms },
		Log:   func(s string) { logs = append(logs, s) },
	})
	followed := []map[string]string{}
	for _, f := range out {
		row := map[string]string{"fullName": f.FullName, "outcome": f.Outcome}
		if f.Outcome == update.NeverStarted || f.Outcome == update.Unknown {
			row["detail"] = f.Detail
		}
		followed = append(followed, row)
	}
	delays := []int64{}
	for _, r := range []int{0, 1, 2, 3, 4, 9} {
		delays = append(delays, update.PollDelay(r))
	}
	return map[string]any{"followed": followed, "sleeps": sleeps, "logs": logs, "pollDelays": delays}, nil
}

// fake is a GH over a calls table keyed "METHOD path": one response, or a
// list answered in order with the last repeated; 404 where the table names
// nothing. It records every call as [method, path, body].
func fake(table map[string]json.RawMessage) (fleet.GH, *[][]any) {
	calls := [][]any{}
	used := map[string]int{}
	type resp struct {
		Status int
		JSON   json.RawMessage
	}
	return func(method, path string, body any) (fleet.Response, error) {
		key := method + " " + path
		if body == nil {
			calls = append(calls, []any{method, path})
		} else {
			b, _ := json.Marshal(body)
			var v any
			_ = json.Unmarshal(b, &v)
			calls = append(calls, []any{method, path, v})
		}
		raw, ok := table[key]
		if !ok {
			return fleet.Response{Status: 404, JSON: json.RawMessage("null")}, nil
		}
		var one resp
		if json.Unmarshal(raw, &one) == nil && one.Status != 0 {
			return fleet.Response{Status: one.Status, JSON: orNull(one.JSON)}, nil
		}
		var list []resp
		if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
			return fleet.Response{}, fmt.Errorf("calls[%q] is neither a response nor a list of them", key)
		}
		i := min(used[key], len(list)-1)
		used[key]++
		return fleet.Response{Status: list[i].Status, JSON: orNull(list[i].JSON)}, nil
	}, &calls
}

func orNull(j json.RawMessage) json.RawMessage {
	if len(j) == 0 {
		return json.RawMessage("null")
	}
	return j
}
