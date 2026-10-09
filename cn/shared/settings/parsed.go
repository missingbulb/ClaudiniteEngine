package settings

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/descriptor"
)

// The settings file parsed whole, as the descriptor parsers read every
// descriptor: the packs block (each declared entry an id or an entry
// object) and the checks block. The engine block stays read by the
// strict patterns above, since the launcher reads the pin with no parser.
//
//	packs:                                checks:
//	  declared:                             rules:
//	    - basics                              hello-declared: "off"
//	    - local/mine                        accept:
//	    - id: git-github                      - rule: some-check
//	      config: {...}                         path: docs/
//	      rules: {some-check: advise}           reason: "why this is fine"
//	      accept: [{rule: x, path: y, reason: z}]

// LocalPrefix marks a declared id as one of the repo's own packs, under
// .claudinite/local/packs/<name>/.
const LocalPrefix = "local/"

// OnFail values a rules override may name.
var OnFailValues = []string{"block", "advise", "off"}

// Acceptance is one accepted finding: a rule, optionally a path (exact, or
// a subtree when it ends in "/"), and the reason that makes it reviewable.
type Acceptance struct {
	Rule   string
	Path   string
	Reason string
	// Pack names the entry the acceptance came from; "" for the top level.
	Pack string
}

// PackEntry is one declared pack.
type PackEntry struct {
	// ID is the bare id: a canon pack's id, or a local pack's name.
	ID     string
	Local  bool
	Object bool
	Config map[string]any
	Rules  map[string]string
	Accept []Acceptance
	// Answers are the adoption questions' answers, verbatim, by question
	// id; cn settings answer is their one writer.
	Answers map[string]string
}

// Token is the entry's id as the declaration spells it.
func (e PackEntry) Token() string {
	if e.Local {
		return LocalPrefix + e.ID
	}
	return e.ID
}

// Checks is the checks block.
type Checks struct {
	Rules  map[string]string
	Accept []Acceptance
}

// Parsed is the settings file's packs, checks, tasks and fleet blocks.
type Parsed struct {
	Packs  Packs
	Checks Checks
	// Tasks is the tasks block as written, nil when absent; the queue's
	// own config package reads its keys.
	Tasks map[string]any
	// Fleet is the fleet block, nil when the file has none: its presence
	// makes the repo a fleet manager.
	Fleet map[string]any
}

var topSchema = descriptor.Schema{Name: "settings", Keys: map[string]descriptor.Kind{
	"engine": descriptor.Object, "packs": descriptor.Object, "checks": descriptor.Object, "tasks": descriptor.Object, "fleet": descriptor.Object,
}}

// fleetSchema is the fleet block: whose repositories the manager covers,
// which it leaves out, what it seeds into each member, when a member
// reads as stale, and the corpus roots beside packs/ a promote may write.
var fleetSchema = descriptor.Schema{Name: "fleet", Keys: map[string]descriptor.Kind{
	"owner": descriptor.String, "kind": descriptor.String, "exclude": descriptor.List, "packSeeds": descriptor.List,
	"staleDays": descriptor.Any, "writePaths": descriptor.List,
}}

var packsSchema = descriptor.Schema{Name: "packs", Keys: map[string]descriptor.Kind{
	"channel": descriptor.String, "declared": descriptor.Any, "sources": descriptor.Any,
}}

var checksSchema = descriptor.Schema{Name: "checks", Keys: map[string]descriptor.Kind{
	"rules": descriptor.Object, "accept": descriptor.List,
}}

var entrySchema = descriptor.Schema{Name: "pack entry", Keys: map[string]descriptor.Kind{
	"id": descriptor.String, "config": descriptor.Object, "rules": descriptor.Object, "accept": descriptor.List,
	"answers": descriptor.Any,
}}

var localIDPattern = regexp.MustCompile(`^local/([A-Za-z0-9][A-Za-z0-9_.-]*)$`)

// ParseFile parses the whole settings file: every block's shape, closed at
// each level. Values a check judges (an override outside block, advise and
// off; an acceptance with no reason) parse, and verify reports them.
func ParseFile(raw []byte, f Format) (Parsed, error) {
	v, err := descriptor.ParseBytes(raw, descriptor.Format(f))
	if err != nil {
		return Parsed{}, fmt.Errorf("%s does not parse: %w", RelPath(f), err)
	}
	if v == nil {
		v = map[string]any{}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return Parsed{}, fmt.Errorf("%s must hold an object", RelPath(f))
	}
	if errs := topSchema.Validate(obj); len(errs) > 0 {
		return Parsed{}, joinErrs(errs)
	}
	p := Parsed{}
	if p.Packs, err = parsePacks(obj); err != nil {
		return Parsed{}, err
	}
	if p.Checks, err = parseChecks(obj["checks"], "checks"); err != nil {
		return Parsed{}, err
	}
	p.Tasks, _ = obj["tasks"].(map[string]any)
	if raw, ok := obj["fleet"]; ok {
		block := raw.(map[string]any)
		if errs := fleetSchema.Validate(block); len(errs) > 0 {
			return Parsed{}, fmt.Errorf("fleet: %w", joinErrs(errs))
		}
		p.Fleet = block
	}
	return p, nil
}

func joinErrs(errs []error) error {
	var s []string
	for _, e := range errs {
		s = append(s, e.Error())
	}
	return errors.New(strings.Join(s, "; "))
}

func parsePacks(obj map[string]any) (p Packs, err error) {
	p = Packs{Channel: ChannelStable}
	raw, present := obj["packs"]
	if !present {
		return p, nil
	}
	p.Present = true
	block := raw.(map[string]any)
	if errs := packsSchema.Validate(block); len(errs) > 0 {
		return Packs{}, fmt.Errorf("packs: %w", joinErrs(errs))
	}
	if c, ok := block["channel"].(string); ok {
		if c != ChannelStable && c != ChannelCanary {
			return Packs{}, fmt.Errorf("packs.channel must be \"stable\" or \"canary\", not %q", c)
		}
		p.Channel = c
	}
	if raw, ok := block["sources"]; ok {
		if p.Sources, err = parseSources(raw); err != nil {
			return Packs{}, err
		}
	}
	list, ok := block["declared"].([]any)
	if !ok && block["declared"] != nil {
		return Packs{}, errors.New("packs: \"declared\" must be a list")
	}
	seen := map[string]bool{}
	for i, e := range list {
		entry, err := parseEntry(e, i)
		if err != nil {
			return Packs{}, err
		}
		if seen[entry.Token()] {
			return Packs{}, fmt.Errorf("packs.declared names %s twice", entry.Token())
		}
		seen[entry.Token()] = true
		p.Entries = append(p.Entries, entry)
		if entry.Local {
			p.Local = append(p.Local, entry.ID)
		} else {
			p.Declared = append(p.Declared, entry.ID)
		}
	}
	return p, nil
}

func parseID(token string) (string, bool, error) {
	if m := localIDPattern.FindStringSubmatch(token); m != nil {
		return m[1], true, nil
	}
	if !PackIDPattern.MatchString(token) {
		return "", false, fmt.Errorf("packs.declared: %q is not a pack id (lowercase letters, digits and dashes) or local/<name>", token)
	}
	return token, false, nil
}

func parseEntry(e any, i int) (PackEntry, error) {
	switch v := e.(type) {
	case string:
		id, local, err := parseID(v)
		return PackEntry{ID: id, Local: local}, err
	case map[string]any:
		if errs := entrySchema.Validate(v); len(errs) > 0 {
			return PackEntry{}, fmt.Errorf("packs.declared[%d]: %w", i, joinErrs(errs))
		}
		token, ok := v["id"].(string)
		if !ok {
			return PackEntry{}, fmt.Errorf("packs.declared[%d] has no \"id\"", i)
		}
		id, local, err := parseID(token)
		if err != nil {
			return PackEntry{}, err
		}
		entry := PackEntry{ID: id, Local: local, Object: true}
		if c, ok := v["config"].(map[string]any); ok {
			entry.Config = c
		}
		if a, ok := v["answers"]; ok && a != nil {
			obj, ok := a.(map[string]any)
			if !ok {
				return PackEntry{}, fmt.Errorf("packs.declared[%d]: the %s entry's answers must be an object of question id to answer text", i, token)
			}
			entry.Answers = map[string]string{}
			for q, t := range obj {
				text, ok := t.(string)
				if !ok {
					return PackEntry{}, fmt.Errorf("packs.declared[%d]: the %s entry's answer to %q must be text", i, token, q)
				}
				entry.Answers[q] = text
			}
		}
		c, err := parseChecks(map[string]any{"rules": v["rules"], "accept": v["accept"]}, "the "+token+" pack entry")
		if err != nil {
			return PackEntry{}, err
		}
		entry.Rules, entry.Accept = c.Rules, c.Accept
		for j := range entry.Accept {
			entry.Accept[j].Pack = id
		}
		return entry, nil
	}
	return PackEntry{}, fmt.Errorf("packs.declared[%d] is neither a pack id nor an entry object", i)
}

func parseChecks(raw any, where string) (Checks, error) {
	c := Checks{}
	obj, _ := raw.(map[string]any)
	if obj == nil {
		return c, nil
	}
	if where == "checks" {
		if errs := checksSchema.Validate(obj); len(errs) > 0 {
			return Checks{}, fmt.Errorf("checks: %w", joinErrs(errs))
		}
	}
	if r, ok := obj["rules"]; ok && r != nil {
		rules, ok := r.(map[string]any)
		if !ok {
			return Checks{}, fmt.Errorf("%s: rules must be an object of rule id to block, advise or off", where)
		}
		c.Rules = map[string]string{}
		for id, val := range rules {
			s, ok := val.(string)
			if !ok {
				return Checks{}, fmt.Errorf("%s: rules.%s must be \"block\", \"advise\" or \"off\"", where, id)
			}
			c.Rules[id] = s
		}
	}
	if a, ok := obj["accept"]; ok && a != nil {
		list, ok := a.([]any)
		if !ok {
			return Checks{}, fmt.Errorf("%s: accept must be a list of {rule, path, reason}", where)
		}
		for i, e := range list {
			m, ok := e.(map[string]any)
			if !ok {
				return Checks{}, fmt.Errorf("%s: accept[%d] must be an object {rule, path, reason}", where, i)
			}
			acc := Acceptance{}
			for k, val := range m {
				s, ok := val.(string)
				switch {
				case k != "rule" && k != "path" && k != "reason":
					return Checks{}, fmt.Errorf("%s: accept[%d] holds %q; an acceptance takes rule, path and reason", where, i, k)
				case !ok:
					return Checks{}, fmt.Errorf("%s: accept[%d].%s must be a string", where, i, k)
				case k == "rule":
					acc.Rule = s
				case k == "path":
					acc.Path = s
				default:
					acc.Reason = s
				}
			}
			if acc.Rule == "" {
				return Checks{}, fmt.Errorf("%s: accept[%d] names no rule", where, i)
			}
			c.Accept = append(c.Accept, acc)
		}
	}
	return c, nil
}

// Effective merges the top-level checks block with every entry's rules and
// accept, as the Node engine's loadConfig merges them: two sources setting
// one rule differently is a conflict, reported and never resolved by
// order.
func (p Parsed) Effective() (map[string]string, []Acceptance, []string) {
	rules := map[string]string{}
	source := map[string]string{}
	var conflicts []string
	merge := func(overrides map[string]string, from string) {
		ids := make([]string, 0, len(overrides))
		for id := range overrides {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			v := overrides[id]
			if have, ok := rules[id]; ok && have != v {
				conflicts = append(conflicts, fmt.Sprintf("rule %q is set to %q by %s and %q by %s", id, have, source[id], v, from))
				continue
			}
			rules[id], source[id] = v, from
		}
	}
	merge(p.Checks.Rules, "the top-level checks block")
	accept := append([]Acceptance{}, p.Checks.Accept...)
	for _, e := range p.Packs.Entries {
		merge(e.Rules, "the "+e.Token()+" pack entry")
		accept = append(accept, e.Accept...)
	}
	return rules, accept, conflicts
}

// Entry returns the declared entry for a bare id, canon or local.
func (p Packs) Entry(id string, local bool) (PackEntry, bool) {
	for _, e := range p.Entries {
		if e.ID == id && e.Local == local {
			return e, true
		}
	}
	return PackEntry{}, false
}
