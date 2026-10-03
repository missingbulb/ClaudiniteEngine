package fleet

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
)

// LocalPackRoot is where a member's own packs live; a window commit
// touching it is a local-pack change.
const LocalPackRoot = ".claudinite/local/packs/"

// SignalMember is one member as the fleet signal carries it.
type SignalMember struct {
	Repo              string         `json:"repo"`
	DefaultBranch     string         `json:"defaultBranch"`
	ActivePacks       []string       `json:"activePacks"`
	PackConfigs       map[string]any `json:"packConfigs"`
	LocalPacksChanged bool           `json:"localPacksChanged"`
	SchedulesItself   bool           `json:"schedulesItself"`
	Stamp             *Stamp         `json:"stamp"`
}

// Stamp is a Node member's provenance stamp; a cn member carries none.
type Stamp struct {
	Updated any `json:"updated"`
	Ref     any `json:"ref"`
}

// Signal is the fleet signal's value: every covered, awake, non-fork,
// non-archived member the owner owns, or the error that left it empty.
type Signal struct {
	Owner   string         `json:"owner"`
	Members []SignalMember `json:"members"`
	Error   string         `json:"error,omitempty"`
}

// ReadFleet is the fleet signal over the token: the owner's members, each
// with its declared packs and their config, whether a default-branch
// commit since touched its local packs, whether it schedules itself, and
// its stamp. An enumeration that finds nothing is an error, never an
// empty fleet.
func ReadFleet(gh GH, owner, since string) Signal {
	owner = strings.ToLower(owner)
	repos, err := Enumerate(gh, owner)
	if err != nil {
		return Signal{Owner: owner, Members: []SignalMember{}, Error: err.Error()}
	}
	out := Signal{Owner: owner, Members: []SignalMember{}}
	for _, r := range repos {
		if r.Archived || r.Fork {
			continue
		}
		m, err := ReadMember(gh, r.FullName, r.Branch())
		if err != nil || !m.Covered() || m.Dormant {
			continue
		}
		sm := SignalMember{Repo: r.FullName, DefaultBranch: r.Branch(), ActivePacks: []string{}, PackConfigs: map[string]any{}}
		switch m.Shape {
		case ShapeNode:
			nodeDeclaration(m.Node, &sm)
		case ShapeCn:
			for _, e := range m.Packs.Entries {
				id := e.ID
				if !e.Local {
					id = workitem.CanonicalPackID(id)
				}
				sm.ActivePacks = append(sm.ActivePacks, id)
				if e.Config != nil {
					sm.PackConfigs[id] = e.Config
				}
			}
			sm.SchedulesItself, _ = FileExists(gh, r.FullName, SchedulerPath)
		}
		if len(sm.ActivePacks) > 0 {
			sm.LocalPacksChanged = localPacksChanged(gh, r.FullName, sm.DefaultBranch, since)
		}
		out.Members = append(out.Members, sm)
	}
	return out
}

// nodeDeclaration reads a Node declaration as the Node signal did: bare
// ids of either entry form, each entry's config, the scheduling cutover
// marker and the stamp.
func nodeDeclaration(decl any, sm *SignalMember) {
	cfg, _ := decl.(map[string]any)
	entries, _ := cfg["packs"].([]any)
	for _, e := range entries {
		raw := ""
		var config any
		hasConfig := false
		switch t := e.(type) {
		case string:
			raw = t
		case map[string]any:
			id, ok := t["id"].(string)
			if !ok {
				continue
			}
			raw = id
			config, hasConfig = t["config"]
		default:
			continue
		}
		id, local := strings.CutPrefix(raw, "local/")
		if !local {
			id = workitem.CanonicalPackID(raw)
		}
		sm.ActivePacks = append(sm.ActivePacks, id)
		if hasConfig {
			sm.PackConfigs[id] = config
		}
	}
	if ts, ok := cfg["taskScheduler"]; ok && ts != nil {
		sm.SchedulesItself = true
	}
	if st, ok := cfg["claudinite"]; ok && st != nil {
		o, _ := st.(map[string]any)
		sm.Stamp = &Stamp{Updated: o["updated"], Ref: o["ref"]}
	}
}

// localPacksChanged scans the window's default-branch commits for one
// touching the local-pack root, stopping at the first; a listing or a
// commit that cannot be read contributes nothing.
func localPacksChanged(gh GH, repo, branch, since string) bool {
	for page := 1; ; page++ {
		r, err := gh.Get(fmt.Sprintf("/repos/%s/commits?sha=%s&since=%s&per_page=100&page=%d", repo, url.QueryEscape(branch), url.QueryEscape(since), page))
		var list []struct {
			SHA string `json:"sha"`
		}
		if err != nil || r.Status != 200 || json.Unmarshal(r.JSON, &list) != nil || len(list) == 0 {
			return false
		}
		for _, c := range list {
			if c.SHA == "" {
				continue
			}
			d, err := gh.Get("/repos/" + repo + "/commits/" + c.SHA)
			var detail struct {
				Files []struct {
					Filename string `json:"filename"`
				} `json:"files"`
			}
			if err != nil || d.Status != 200 || json.Unmarshal(d.JSON, &detail) != nil {
				continue
			}
			for _, f := range detail.Files {
				if strings.HasPrefix(f.Filename, LocalPackRoot) {
					return true
				}
			}
		}
		if len(list) < 100 {
			return false
		}
	}
}
