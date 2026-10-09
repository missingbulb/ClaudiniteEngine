package main

import (
	"encoding/json"
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/update"
)

// updateDecide answers one update-face fixture: core names the decision and raw
// is its input, in the Node engine's shape.
func updateDecide(core string, raw []byte) (any, error) {
	switch core {
	case "plan":
		var in struct {
			Packs     []update.ShelfPack `json:"packs"`
			Declared  []json.RawMessage  `json:"declared"`
			Installed *struct {
				PackVersions map[string]string `json:"packVersions"`
			} `json:"installed"`
			EngineVersion string `json:"engineVersion"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		var declared []string
		for _, d := range in.Declared {
			var id string
			if json.Unmarshal(d, &id) != nil {
				var obj struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(d, &obj)
				id = obj.ID
			}
			if id != "" {
				declared = append(declared, id)
			}
		}
		installed := map[string]string{}
		if in.Installed != nil {
			installed = in.Installed.PackVersions
		}
		return update.PlanPacks(in.Packs, declared, installed, in.EngineVersion), nil
	case "gap":
		return update.RecordsInGap(), nil
	case "delivery":
		var in struct {
			SelftestOK bool   `json:"selftestOk"`
			Delivery   string `json:"delivery"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return update.DeliveryDecision(in.SelftestOK, in.Delivery), nil
	case "applystage":
		return update.ApplyStageFor(), nil
	case "terminal":
		var in struct {
			Outcome *update.Outcome `json:"outcome"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return update.TerminalFor(in.Outcome), nil
	case "convergescope":
		var in struct {
			Files []string `json:"files"`
			Edits []struct {
				Before *string `json:"before"`
				After  *string `json:"after"`
			} `json:"edits"`
			Checkout *struct {
				Head    map[string]*string `json:"head"`
				Working map[string]*string `json:"working"`
			} `json:"checkout"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		out := struct {
			Bookkeeping []bool   `json:"bookkeeping"`
			StampOnly   []bool   `json:"stampOnly"`
			TestVisible []string `json:"testVisible"`
		}{Bookkeeping: []bool{}, StampOnly: []bool{}}
		for _, f := range in.Files {
			out.Bookkeeping = append(out.Bookkeeping, update.IsConvergeBookkeeping(f))
		}
		for _, e := range in.Edits {
			out.StampOnly = append(out.StampOnly, update.PinOnlyEdit(e.Before, e.After, settings.JSON))
		}
		if in.Checkout != nil {
			out.TestVisible = update.TestVisible(in.Checkout.Head, in.Checkout.Working)
		}
		return out, nil
	case "pulltext":
		var in struct {
			Engine *struct {
				From *string `json:"from"`
				To   *string `json:"to"`
			} `json:"engine"`
			Packs *struct {
				Plan []update.PackPlan `json:"plan"`
			} `json:"packs"`
			Terminal json.RawMessage   `json:"terminal"`
			Amends   []json.RawMessage `json:"amends"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		out := struct {
			Titles []string `json:"titles"`
			Amends []bool   `json:"amends"`
		}{Titles: []string{}, Amends: []bool{}}
		var term struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(in.Terminal, &term)
		if len(in.Terminal) > 0 && string(in.Terminal) != "null" && term.Action != update.NeedsHuman {
			var from, to *string
			if in.Engine != nil {
				from, to = in.Engine.From, in.Engine.To
			}
			var plan []update.PackPlan
			if in.Packs != nil {
				plan = in.Packs.Plan
			}
			out.Titles = update.PullTitles(0, from, to, plan)
		}
		for range in.Amends {
			out.Amends = append(out.Amends, false)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown update core %q (plan, gap, delivery, applystage, terminal, convergescope, pulltext)", core)
}
