// Package node reads the Node engine's declaration, .claudinite-settings.json,
// into cn's settings blocks: the packs block, the checks block and the
// claudinite-tasks entry's config. Every key of the Node file is mapped,
// carried, dropped with a reason or refused, and the report says which,
// one line per top-level key and per pack-entry key. It is pure: bytes in,
// blocks and a report out; `cn settings import` writes them.
package node

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// File is the Node engine's declaration, relative to the repo root.
const File = ".claudinite-settings.json"

// TasksPack is the pack whose entry config holds the scheduler's and the
// landing lane's member settings.
const TasksPack = "claudinite-tasks"

// Renamed is the Node engine's map of a retired canon pack id to today's
// (engine/pack_loader/renamed-packs.mjs at the freeze): absorbed packs and
// renamed ones alike. Only the import rewrites a declaration by it; cn's
// readers match ids literally, and verify's pack-declared names today's
// id from it.
var Renamed = map[string]string{
	"barriers":       "basics",
	"tidy-repo":      "basics",
	"static-website": "public-website",
}

// Absorbed are the renamed ids whose pack was absorbed rather than renamed,
// each with the answers retired with its questions: the Node engine's
// absorbedPackConfig records (the lifecycle pack's barriers-absorbed
// migration at the freeze). An absorbed entry's config nests under its old
// id on the survivor, where the survivor reads it.
var Absorbed = map[string]AbsorbedSpec{
	"barriers": {DropAnswers: []string{"goals"}},
}

// RetiredOnFail maps the Node engine's severity spelling (its
// LEGACY_ON_FAIL) to cn's on_fail, which alone cn reads: in a rules
// override and in a declared check alike.
var RetiredOnFail = map[string]string{"blocking": "block", "advisory": "advise"}

// RetiredManifestKeys are the Node manifest fields cn's pack manifest does
// not hold: the fingerprint relevanceDetector replaced, the pack
// contributions, and the coded rule lists.
var RetiredManifestKeys = []string{"detect", "marker", "contributes", "contributedRules", "worldRules", "workRules"}

// AbsorbedSpec is one absorbedPackConfig record.
type AbsorbedSpec struct {
	DropAnswers []string
}

// Tree answers what the import needs to know of the member's tree.
type Tree interface {
	// HasLocal reports whether .claudinite/local/packs/<name>/ exists.
	HasLocal(name string) bool
}

// Kind is what the import did with one key.
type Kind string

const (
	Mapped  Kind = "mapped"
	Carried Kind = "carried"
	Dropped Kind = "dropped"
	Refused Kind = "refused"
)

// Line is one line of the report.
type Line struct {
	Kind Kind
	// Key is the Node key, To where it went, Why the reason.
	Key, To, Why string
}

func (l Line) String() string {
	s := string(l.Kind) + " " + l.Key
	if l.To != "" {
		s += " → " + l.To
	}
	if l.Why != "" {
		s += ": " + l.Why
	}
	return s
}

// Report is the import's account of every key.
type Report []Line

// Refused reports whether any key was refused, in which case nothing may
// be written.
func (r Report) Refused() bool {
	for _, l := range r {
		if l.Kind == Refused {
			return true
		}
	}
	return false
}

func (r Report) String() string {
	var b strings.Builder
	for _, l := range r {
		b.WriteString(l.String() + "\n")
	}
	return b.String()
}

// Decl is cn's reading of the declaration: the packs block (declared
// entries in order, the claudinite-tasks config on its entry) and the
// checks block, nil when the Node file had neither rules nor accept.
type Decl struct {
	Packs  *settings.Ordered
	Checks *settings.Ordered
}

// Blocks are the blocks the import writes.
func (d Decl) Blocks() []settings.Block {
	out := []settings.Block{{Name: "packs", Value: d.Packs}}
	if d.Checks != nil {
		out = append(out, settings.Block{Name: "checks", Value: d.Checks})
	}
	return out
}

var (
	localToken   = regexp.MustCompile(`^local/([A-Za-z0-9][A-Za-z0-9_.-]*)$`)
	taskIDForm   = regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)
	localDirName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// retiredTop are the top-level keys the Node engine itself stopped
// reading before the freeze (#1640): both engines refuse them.
var retiredTop = map[string]string{
	"claudinite":  "the retired stamp block (#1640), which the Node engine refuses too; cn's pin is engine.version and a pack's version is its vendored pack.json's",
	"maintenance": "the retired delivery block (#1640), which the Node engine refuses too; the update auto-merges (record row 71)",
	"packConfig":  "the retired top-level pack parameters (#1640), which the Node engine refuses too; a pack's parameters are its entry's config",
}

type importer struct {
	tree    Tree
	report  Report
	entries []*entry
	byToken map[string]*entry
}

type entry struct {
	token string
	obj   *settings.Ordered // nil while the entry is a bare token
}

func (e *entry) object() *settings.Ordered {
	if e.obj == nil {
		e.obj = settings.NewOrdered()
		e.obj.Set("id", e.token)
	}
	return e.obj
}

func (e *entry) value() any {
	if e.obj == nil || e.obj.Len() == 1 {
		return e.token
	}
	return e.obj
}

func (im *importer) add(k Kind, key, to, why string) {
	im.report = append(im.report, Line{Kind: k, Key: key, To: to, Why: why})
}

// Read maps the Node declaration's bytes. An error is a file that is not
// a JSON object; a key the import cannot carry is a refused line, and the
// Decl is then not to be written.
func Read(raw []byte, tree Tree) (Decl, Report, error) {
	v, err := settings.DecodeOrdered(raw)
	if err != nil {
		return Decl{}, nil, fmt.Errorf("%s is not valid JSON: %w", File, err)
	}
	top, ok := v.(*settings.Ordered)
	if !ok {
		return Decl{}, nil, fmt.Errorf("%s must hold a JSON object", File)
	}
	im := &importer{tree: tree, byToken: map[string]*entry{}}
	if p, ok := top.Get("packs"); ok {
		im.packs(p)
	}
	var checks *settings.Ordered
	for _, k := range top.Keys() {
		val, _ := top.Get(k)
		switch k {
		case "packs":
		case "rules":
			obj, ok := val.(*settings.Ordered)
			if !ok {
				im.add(Refused, "rules", "", "must be an object of rule id to off, advise or block")
				continue
			}
			if checks == nil {
				checks = settings.NewOrdered()
			}
			checks.Set("rules", im.rules(obj, "rules"))
			im.add(Mapped, "rules", "checks.rules", "")
		case "accept":
			list, ok := val.([]any)
			if !ok {
				im.add(Refused, "accept", "", "must be a list of {rule, path, reason}")
				continue
			}
			if checks == nil {
				checks = settings.NewOrdered()
			}
			checks.Set("accept", list)
			im.add(Mapped, "accept", "checks.accept", "")
		case "sharedConstants":
			im.sharedConstants(val)
		case "engineVersion":
			im.add(Dropped, "engineVersion", "", "the pin is engine.version in .claudinite/settings.*")
		case "taskScheduler":
			im.taskScheduler(val)
		case "dailyClaudiniteUpdatesRequirePrReview", "dormant":
			im.tasksFlag(k, val)
		case "servedBy":
			im.add(Dropped, "servedBy", "", "cn has one update mechanism (record row 73)")
		default:
			if why, ok := retiredTop[k]; ok {
				im.add(Refused, k, "", why)
				continue
			}
			im.add(Refused, k, "", "not a setting the Node engine reads either; delete it")
		}
	}
	packs := settings.NewOrdered()
	declared := []any{}
	for _, e := range im.entries {
		declared = append(declared, e.value())
	}
	packs.Set("declared", declared)
	return Decl{Packs: packs, Checks: checks}, im.report, nil
}

func (im *importer) packs(v any) {
	list, ok := v.([]any)
	if !ok {
		im.add(Refused, "packs", "", "must be a list of pack ids and entry objects")
		return
	}
	for i, raw := range list {
		key := fmt.Sprintf("packs[%d]", i)
		switch x := raw.(type) {
		case string:
			im.declare(key, x, nil)
		case *settings.Ordered:
			id, ok := x.Get("id")
			s, isString := id.(string)
			if !ok || !isString {
				im.add(Refused, key, "", "an entry object names its pack in a string \"id\"")
				continue
			}
			im.declare(key, s, x)
		default:
			im.add(Refused, key, "", "neither a pack id nor an entry object")
		}
	}
}

// token is cn's spelling of a Node id, and why it changed, or "" when the
// id is no pack id at all.
func (im *importer) token(id string) (string, string) {
	if localToken.MatchString(id) {
		return id, ""
	}
	if to, ok := Renamed[id]; ok {
		return to, "renamed or absorbed into " + to + " (record row 80)"
	}
	if settings.PackIDPattern.MatchString(id) {
		if im.tree != nil && im.tree.HasLocal(id) {
			return settings.LocalPrefix + id, "a local pack declared by its bare name"
		}
		return id, ""
	}
	if localDirName.MatchString(id) && im.tree != nil && im.tree.HasLocal(id) {
		return settings.LocalPrefix + id, "a local pack declared by its bare name"
	}
	return "", ""
}

func (im *importer) declare(key, id string, obj *settings.Ordered) {
	tok, why := im.token(id)
	if tok == "" {
		reason := "not a pack id (lowercase letters, digits and dashes) or local/<name>"
		if strings.HasPrefix(id, "local_packs/") {
			reason = "the local_packs/ prefix is retired (#1640) and the Node engine activates nothing for it; write local/<name>"
		}
		im.add(Refused, key, "", fmt.Sprintf("%q is %s", id, reason))
		return
	}
	have, dup := im.byToken[tok]
	if dup {
		why = joinWhy(why, "merged into the entry declared before it, whose own values win")
	}
	im.add(Mapped, fmt.Sprintf("%s %q", key, id), fmt.Sprintf("packs.declared %q", tok), why)
	e := &entry{token: tok}
	spec, absorbed := Absorbed[id]
	if obj != nil {
		for _, k := range obj.Keys() {
			val, _ := obj.Get(k)
			sub := key + "." + k
			switch k {
			case "id":
			case "version":
				im.add(Dropped, sub, "", "the installed version is the vendored pack.json's")
			case "config":
				if _, ok := val.(*settings.Ordered); !ok {
					im.add(Refused, sub, "", "must be an object of the pack's parameters")
					continue
				}
				if absorbed {
					nested := settings.NewOrdered()
					nested.Set(id, val)
					e.object().Set("config", nested)
					im.add(Mapped, sub, fmt.Sprintf("packs.declared %q config.%s", tok, id), "an absorbed pack's parameters nest under its old id")
					continue
				}
				e.object().Set("config", val)
				im.add(Mapped, sub, fmt.Sprintf("packs.declared %q config", tok), "")
			case "rules":
				r, ok := val.(*settings.Ordered)
				if !ok {
					im.add(Refused, sub, "", "must be an object of rule id to off, advise or block")
					continue
				}
				e.object().Set("rules", im.rules(r, sub))
				im.add(Mapped, sub, fmt.Sprintf("packs.declared %q rules", tok), "")
			case "accept":
				if _, ok := val.([]any); !ok {
					im.add(Refused, sub, "", "must be a list of {rule, path, reason}")
					continue
				}
				e.object().Set("accept", val)
				im.add(Mapped, sub, fmt.Sprintf("packs.declared %q accept", tok), "")
			case "answers":
				if a, ok := val.(*settings.Ordered); ok && absorbed && len(spec.DropAnswers) > 0 {
					kept := settings.NewOrdered()
					for _, q := range a.Keys() {
						ans, _ := a.Get(q)
						if contains(spec.DropAnswers, q) {
							im.add(Dropped, sub+"."+q, "", "its question was retired with the absorbed pack")
							continue
						}
						kept.Set(q, ans)
					}
					if kept.Len() > 0 {
						e.object().Set(k, kept)
						im.add(Carried, sub, "", "")
					}
					continue
				}
				e.object().Set(k, val)
				im.add(Carried, sub, "", "")
			case "via":
				im.add(Dropped, sub, "", "cn requires every pack a declared one needs to be declared itself, so no entry records who pulled it in")
			default:
				im.add(Refused, sub, "", "not a pack-entry property the Node engine reads either; delete it")
			}
		}
	}
	if dup {
		if e.obj != nil {
			merge(have.object(), e.obj)
		}
		return
	}
	im.byToken[tok] = e
	im.entries = append(im.entries, e)
}

func joinWhy(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// merge folds absorbed into survivor as the Node engine's
// mergeDeclarationEntries did: a key the survivor lacks is taken whole;
// two lists concatenate, dropping an element the survivor already holds;
// two objects merge one level, the survivor's keys winning; a scalar the
// survivor holds stands.
func merge(survivor, absorbed *settings.Ordered) {
	for _, k := range absorbed.Keys() {
		if k == "id" {
			continue
		}
		val, _ := absorbed.Get(k)
		have, ok := survivor.Get(k)
		if !ok {
			survivor.Set(k, val)
			continue
		}
		switch h := have.(type) {
		case *settings.Ordered:
			if a, ok := val.(*settings.Ordered); ok {
				for _, ak := range a.Keys() {
					if _, own := h.Get(ak); !own {
						av, _ := a.Get(ak)
						h.Set(ak, av)
					}
				}
			}
		case []any:
			if a, ok := val.([]any); ok {
				seen := map[string]bool{}
				for _, x := range h {
					seen[canonical(x)] = true
				}
				for _, x := range a {
					if !seen[canonical(x)] {
						h = append(h, x)
					}
				}
				survivor.Set(k, h)
			}
		}
	}
}

func canonical(v any) string {
	b, _ := json.Marshal(settings.Plain(v))
	return string(b)
}

// rules copies an overrides object, rewriting the retired spelling.
func (im *importer) rules(r *settings.Ordered, key string) *settings.Ordered {
	out := settings.NewOrdered()
	for _, id := range r.Keys() {
		v, _ := r.Get(id)
		if s, ok := v.(string); ok {
			if to, retired := RetiredOnFail[s]; retired {
				im.add(Mapped, fmt.Sprintf("%s.%s %q", key, id, s), fmt.Sprintf("%q", to), "the retired severity spelling")
				v = to
			}
		}
		out.Set(id, v)
	}
	return out
}

func (im *importer) entry(token string) *entry {
	return im.byToken[token]
}

func (im *importer) sharedConstants(v any) {
	b := im.entry("basics")
	if b == nil {
		im.add(Dropped, "sharedConstants", "", "basics is not declared, and nothing else reads it (record row 51)")
		return
	}
	cfg := configOf(b)
	if _, ok := cfg.Get("sharedConstants"); ok {
		im.add(Dropped, "sharedConstants", "", "the basics entry's config already holds sharedConstants, which wins")
		return
	}
	cfg.Set("sharedConstants", v)
	im.add(Mapped, "sharedConstants", `packs.declared "basics" config.sharedConstants`, "")
}

func configOf(e *entry) *settings.Ordered {
	o := e.object()
	if c, ok := o.Get("config"); ok {
		if co, ok := c.(*settings.Ordered); ok {
			return co
		}
	}
	c := settings.NewOrdered()
	o.Set("config", c)
	return c
}

// toTasks moves a scheduler or delivery setting onto the claudinite-tasks
// entry's config, where cn reads it; empty says the value carries nothing.
func (im *importer) toTasks(key, cfgKey string, v any, empty bool) {
	t := im.entry(TasksPack)
	if t == nil {
		if empty {
			im.add(Dropped, key, "", TasksPack+" is not declared, and the value says nothing")
			return
		}
		im.add(Refused, key, "", TasksPack+" is not declared, so nothing would read it; declare "+TasksPack+" or delete the key")
		return
	}
	cfg := configOf(t)
	if _, ok := cfg.Get(cfgKey); ok {
		im.add(Dropped, key, "", "the "+TasksPack+" entry's config already says "+cfgKey+", which wins")
		return
	}
	cfg.Set(cfgKey, v)
	im.add(Mapped, key, fmt.Sprintf("packs.declared %q config.%s", TasksPack, cfgKey), "")
}

func (im *importer) tasksFlag(key string, v any) {
	b, ok := v.(bool)
	if !ok {
		im.add(Refused, key, "", "must be true or false")
		return
	}
	im.toTasks(key, key, b, !b)
}

func (im *importer) taskScheduler(v any) {
	ts, ok := v.(*settings.Ordered)
	if !ok {
		im.add(Refused, "taskScheduler", "", "must be an object")
		return
	}
	for _, k := range ts.Keys() {
		val, _ := ts.Get(k)
		key := "taskScheduler." + k
		switch k {
		case "agenticTaskInvocationEndpoints":
			eps, ok := val.(*settings.Ordered)
			if !ok {
				im.add(Refused, key, "", "must be an object of endpoint name to {url, tokenSecret}")
				continue
			}
			bad := ""
			for _, name := range eps.Keys() {
				ep, _ := eps.Get(name)
				o, ok := ep.(*settings.Ordered)
				url, _ := get(o, "url").(string)
				secret, _ := get(o, "tokenSecret").(string)
				if !ok || url == "" || secret == "" {
					bad = name
					break
				}
			}
			if bad != "" {
				im.add(Refused, key, "", fmt.Sprintf("the endpoint %q must be {url, tokenSecret}, both strings", bad))
				continue
			}
			im.toTasks(key, k, val, eps.Len() == 0)
		case "disabledTasks":
			list, ok := val.([]any)
			for _, x := range list {
				s, isString := x.(string)
				ok = ok && isString && taskIDForm.MatchString(s)
			}
			if !ok {
				im.add(Refused, key, "", `must be a list of "<pack>/<task>" ids`)
				continue
			}
			im.toTasks(key, k, val, len(list) == 0)
		case "dispatch":
			switch val {
			case "queue":
				im.add(Dropped, key, "", "the work-item queue is the only dispatch")
			case "slots":
				im.add(Refused, key, "", `"slots" named the deleted slot scheduler, and the Node engine refuses it too; delete the key`)
			default:
				im.add(Refused, key, "", `must be "queue", or absent`)
			}
		case "dailyHour", "weeklyDay", "monthlyDay":
			im.add(Dropped, key, "", "a cadence measures whole UTC periods (record row 73)")
		case "endpoints":
			im.add(Refused, key, "", "the retired spelling of agenticTaskInvocationEndpoints (#1640), which the Node engine refuses too; rename it")
		default:
			im.add(Refused, key, "", "not a taskScheduler setting the Node engine reads either; delete it")
		}
	}
}

func get(o *settings.Ordered, k string) any {
	if o == nil {
		return nil
	}
	v, _ := o.Get(k)
	return v
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
