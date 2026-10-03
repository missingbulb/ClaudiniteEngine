// Package addpacks is the fleet-add-missing-packs sweep: the scan, which
// fingerprints every member's tree against the shelf's catalog and places
// a suspected work list where its declaration lacks what its files
// suggest, and the force, which places a requested work list in each repo
// a person named. Both write an issue into the member, marked for the
// member's own adopt-requested-packs task, and nudge its scheduler; the
// adoption itself happens in the member, one reviewed pull request there.
package addpacks

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/jsregex"
)

// AllMembers is the keyword a caller sends for every covered, awake
// member under the owner: the scan's fleet-wide scope, said out loud.
const AllMembers = "all-covered-members"

// ScheduledArgv is the scheduled run's command line, as the task's
// code_work spells it.
var ScheduledArgv = []string{"--scan-for-needed-packs=true", "--repos=" + AllMembers}

// Params is what a run is, resolved from its command line and its
// item's Context. Repos is nil when AllMembers is set.
type Params struct {
	Scan        bool     `json:"scan"`
	AllMembers  bool     `json:"allMembers"`
	Repos       []string `json:"repos"`
	AddPacks    []string `json:"addPacks"`
	PackConfig  *Obj     `json:"packConfig"`
	PackAnswers *Obj     `json:"packAnswers"`
	Forced      bool     `json:"forced"`
}

// list splits a space-separated value, as JavaScript's /\s+/ does.
func list(raw string) []string {
	out := []string{}
	for _, s := range strings.FieldsFunc(raw, jsregex.IsSpace) {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

var argRE = regexp.MustCompile(`^--([a-z][a-z0-9-]*)=(.*)$`)

// ParseArgv reads the --name=value pairs a code_work line carries.
func ParseArgv(argv []string) (map[string]string, error) {
	out := map[string]string{}
	for _, a := range argv {
		m := argRE.FindStringSubmatch(a)
		if m == nil {
			return nil, fmt.Errorf("unrecognised code_work argument `%s` — expected `--<name>=<value>`", a)
		}
		out[m[1]] = m[2]
	}
	return out, nil
}

func boolParam(name, raw string) (bool, error) {
	switch strings.ToLower(jsregex.Trim(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%s must be exactly `true` or `false` (got `%s`)", name, raw)
}

// foldEntries folds `<pack>.<dotted.key>=<value>` entries into one object
// per pack; two entries that would set one key are refused.
func foldEntries(kind string, raws []string) (*Obj, error) {
	out := NewObj()
	for _, raw := range raws {
		text := jsregex.Trim(raw)
		eq := strings.Index(text, "=")
		if eq == -1 {
			return nil, fmt.Errorf("%s entry `%s` is not `<pack-id>.<key>=<value>`", kind, raw)
		}
		path, value := jsregex.Trim(text[:eq]), jsregex.Trim(text[eq+1:])
		dot := strings.Index(path, ".")
		if dot <= 0 || dot == len(path)-1 {
			return nil, fmt.Errorf("%s entry `%s` names no pack — expected `<pack-id>.<key>=<value>`", kind, raw)
		}
		if value == "" {
			return nil, fmt.Errorf("%s entry `%s` has an empty value", kind, raw)
		}
		pack, keyPath := path[:dot], strings.Split(path[dot+1:], ".")
		conflict := fmt.Errorf("%s entries conflict on `%s.%s`", kind, pack, strings.Join(keyPath, "."))
		v, ok := out.Get(pack)
		if !ok {
			v = NewObj()
			out.Set(pack, v)
		}
		node := v.(*Obj)
		for _, k := range keyPath[:len(keyPath)-1] {
			child, ok := node.Get(k)
			if !ok {
				child = NewObj()
				node.Set(k, child)
			}
			next, isObj := child.(*Obj)
			if !isObj {
				return nil, conflict
			}
			node = next
		}
		leaf := keyPath[len(keyPath)-1]
		if _, ok := node.Get(leaf); ok {
			return nil, conflict
		}
		node.Set(leaf, value)
	}
	return out, nil
}

func prefixed(params map[string]string, prefix string) []string {
	var keys []string
	for k := range params {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := []string{}
	for _, k := range keys {
		out = append(out, params[k])
	}
	return out
}

// ParseParams is the run's whole contract. Neither parameter has a
// default: the scheduled run states both on its command line, a forced
// run in its Context, and Context wins. Every refusal's message is the
// fix.
func ParseParams(argv []string, params map[string]string) (Params, error) {
	cli, err := ParseArgv(argv)
	if err != nil {
		return Params{}, err
	}
	pick := func(flag, key string) (string, bool) {
		if v, ok := params[key]; ok {
			return v, true
		}
		v, ok := cli[flag]
		return v, ok
	}
	rawScan, ok := pick("scan-for-needed-packs", "SCAN_FOR_NEEDED_PACKS")
	if !ok {
		return Params{}, fmt.Errorf("scan_for_needed_packs was not sent. This parameter has no default: the weekly run " +
			"sends `--scan-for-needed-packs=true` from task.json, and a forced run sends " +
			"`--context \"SCAN_FOR_NEEDED_PACKS=false\"` when the item is created.")
	}
	scan, err := boolParam("scan_for_needed_packs", rawScan)
	if err != nil {
		return Params{}, err
	}
	rawRepos, ok := pick("repos", "REPOS")
	names := list(rawRepos)
	if !ok || len(names) == 0 {
		return Params{}, fmt.Errorf("repos was not sent. This parameter has no default: send `%s` for the whole "+
			"fleet (what the weekly run sends) or a space-separated list of repo names (bare `Name` or "+
			"`owner/Name`). Space-separated, not comma-separated — the parameter bag splits keys on commas.", AllMembers)
	}
	all := false
	for _, n := range names {
		if n == AllMembers {
			all = true
		}
	}
	if all && len(names) > 1 {
		return Params{}, fmt.Errorf("repos mixes `%s` with explicit names (%s) — send one or the other", AllMembers, strings.Join(names, ", "))
	}
	rawAdd, _ := pick("add-packs", "ADD_PACKS")
	addPacks := list(rawAdd)
	packConfig, err := foldEntries("PACK_CONFIG", prefixed(params, "PACK_CONFIG"))
	if err != nil {
		return Params{}, err
	}
	packAnswers, err := foldEntries("PACK_ANSWER", prefixed(params, "PACK_ANSWER"))
	if err != nil {
		return Params{}, err
	}
	if !scan && len(addPacks) == 0 {
		return Params{}, fmt.Errorf("this run would do nothing: scan_for_needed_packs is false and no packs were named. " +
			"Send `ADD_PACKS=<pack-id> …` to force an addition, or `SCAN_FOR_NEEDED_PACKS=true` to sweep.")
	}
	if len(addPacks) > 0 && all {
		return Params{}, fmt.Errorf("forcing packs onto `%s` is refused — name the repos explicitly "+
			"(a mass addition is bounded by the list a human typed, never by a keyword)", AllMembers)
	}
	for _, o := range []*Obj{packConfig, packAnswers} {
		for _, pack := range o.Keys() {
			if !contains(addPacks, pack) {
				named := strings.Join(addPacks, ", ")
				if named == "" {
					named = "empty"
				}
				cfg, _ := o.Get(pack)
				return Params{}, fmt.Errorf("config/answers were sent for `%s`, which is not in ADD_PACKS (%s) — nothing would apply them (%s)", pack, named, Stringify(cfg, ""))
			}
		}
	}
	p := Params{Scan: scan, AllMembers: all, AddPacks: addPacks, PackConfig: packConfig, PackAnswers: packAnswers, Forced: len(params) > 0}
	if !all {
		p.Repos = names
	}
	return p, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
