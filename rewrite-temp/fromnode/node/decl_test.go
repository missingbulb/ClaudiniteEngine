package node

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

var update = flag.Bool("update", false, "rewrite testdata/<fixture>.report from the import")

// The parity settings face's fixtures are the declarations under test:
// one Node declaration and the member tree beside it each.
const fixtures = "../../parity/testdata/settings"

type dirTree string

func (d dirTree) HasLocal(name string) bool {
	st, err := os.Stat(filepath.Join(string(d), ".claudinite/local/packs", name))
	return err == nil && st.IsDir()
}

func readFixture(t *testing.T, name string) (Decl, Report) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, name, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, r, err := Read(raw, dirTree(filepath.Join(fixtures, name, "tree")))
	if err != nil {
		t.Fatal(err)
	}
	return d, r
}

// Each fixture's report, line for line, is testdata/<fixture>.report.
func TestReportPerFixture(t *testing.T) {
	dirs, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 14 {
		t.Fatalf("%d fixtures", len(dirs))
	}
	for _, d := range dirs {
		_, r := readFixture(t, d.Name())
		golden := filepath.Join("testdata", d.Name()+".report")
		if *update {
			if err := os.WriteFile(golden, []byte(r.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v; run go test -update", err)
		}
		if r.String() != string(want) {
			t.Errorf("%s:\ngot\n%s\nwant\n%s", d.Name(), r, want)
		}
	}
}

// Every key of every fixture is accounted for: one line per top-level key
// other than packs, one per entry and one per entry key besides id.
func TestEveryKeyHasALine(t *testing.T) {
	dirs, _ := os.ReadDir(fixtures)
	for _, d := range dirs {
		raw, _ := os.ReadFile(filepath.Join(fixtures, d.Name(), "settings.json"))
		v, err := settings.DecodeOrdered(raw)
		if err != nil {
			t.Fatal(err)
		}
		top := v.(*settings.Ordered)
		want := 0
		for _, k := range top.Keys() {
			val, _ := top.Get(k)
			switch k {
			case "packs":
				for _, e := range val.([]any) {
					want++
					if o, ok := e.(*settings.Ordered); ok {
						want += o.Len() - 1
					}
				}
			case "taskScheduler":
				want += val.(*settings.Ordered).Len()
			default:
				want++
			}
		}
		_, r := readFixture(t, d.Name())
		rewrites := 0
		for _, l := range r {
			if strings.Contains(l.Why, "the retired severity spelling") || strings.Contains(l.Key, ".answers.") {
				rewrites++
			}
		}
		if len(r)-rewrites != want {
			t.Errorf("%s: %d lines for %d keys:\n%s", d.Name(), len(r)-rewrites, want, r)
		}
	}
}

func TestRenamedIDsMerge(t *testing.T) {
	d, r := readFixture(t, "renamed-ids")
	got := settings.Plain(d.Packs)
	want := map[string]any{"declared": []any{
		map[string]any{"id": "basics", "rules": map[string]any{"reference-integrity": "advise"}},
		"public-website", "claudinite-lifecycle",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v\n%s", got, r)
	}
}

// An absorbed pack's entry is reshaped before it merges, as the Node
// engine's barriers-absorbed record did: its config nests under the old id
// and the answer to its retired question goes; then the two entries merge
// one level deep, the survivor's keys winning.
func TestAbsorbedEntryNestsBeforeItMerges(t *testing.T) {
	d, r := readFixture(t, "renamed-ids-config")
	basics := settings.Plain(d.Packs).(map[string]any)["declared"].([]any)[0].(map[string]any)
	cfg := basics["config"].(map[string]any)
	if _, ok := cfg["sharedConstants"]; !ok {
		t.Errorf("the survivor's own config is lost: %v\n%s", cfg, r)
	}
	nested, ok := cfg["barriers"].(map[string]any)
	if !ok || nested["rules"] == nil || cfg["rules"] != nil {
		t.Errorf("barriers' config did not nest under config.barriers: %v\n%s", cfg, r)
	}
	if !reflect.DeepEqual(basics["answers"], map[string]any{"kept": "yes"}) {
		t.Errorf("answers: %v", basics["answers"])
	}
	if _, ok := basics["via"]; ok {
		t.Errorf("via names the survivor itself: %v", basics["via"])
	}

	survivor := settings.NewOrdered()
	inner := settings.NewOrdered()
	inner.Set("a", 1.0)
	deep := settings.NewOrdered()
	deep.Set("x", 1.0)
	inner.Set("deep", deep)
	survivor.Set("config", inner)
	absorbed := settings.NewOrdered()
	other := settings.NewOrdered()
	other.Set("a", 2.0)
	other.Set("b", 2.0)
	deep2 := settings.NewOrdered()
	deep2.Set("y", 2.0)
	other.Set("deep", deep2)
	absorbed.Set("config", other)
	merge(survivor, absorbed)
	want := map[string]any{"config": map[string]any{"a": 1.0, "b": 2.0, "deep": map[string]any{"x": 1.0}}}
	if got := settings.Plain(survivor); !reflect.DeepEqual(got, want) {
		t.Errorf("merge is one level, the survivor's keys winning: %v", got)
	}
}

// The queue's settings move onto the tasks block, under its names, and the
// retired claudinite-tasks entry is not declared.
func TestSchedulerMovesOntoTheTasksBlock(t *testing.T) {
	d, r := readFixture(t, "scheduler")
	got, _ := settings.Plain(d.Tasks).(map[string]any)
	if _, ok := got["routines"]; !ok || !reflect.DeepEqual(got["disabled"], []any{"claudinite-lifecycle/update"}) || len(got) != 2 {
		t.Errorf("%v\n%s", got, r)
	}
	for name, want := range map[string]map[string]any{"review": {"delivery": "review"}, "dormant-top-level": {"dormant": true}} {
		d, r := readFixture(t, name)
		if got := settings.Plain(d.Tasks); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v\n%s", name, got, r)
		}
		if got := settings.Plain(d.Packs).(map[string]any)["declared"]; !reflect.DeepEqual(got, []any{"claudinite-lifecycle"}) {
			t.Errorf("%s: declared %v", name, got)
		}
	}
}

func TestSharedConstantsMoveOrDrop(t *testing.T) {
	d, _ := readFixture(t, "top-level-checks")
	basics := settings.Plain(d.Packs).(map[string]any)["declared"].([]any)[0].(map[string]any)
	if _, ok := basics["config"].(map[string]any)["sharedConstants"]; !ok {
		t.Errorf("basics: %v", basics)
	}
	_, r, _ := Read([]byte(`{"packs": ["node"], "sharedConstants": []}`), nil)
	if r[len(r)-1].Kind != Dropped {
		t.Errorf("%s", r)
	}
}

func TestLocalBareBecomesNamespaced(t *testing.T) {
	d, _ := readFixture(t, "local-bare")
	if got := settings.Plain(d.Packs).(map[string]any)["declared"]; !reflect.DeepEqual(got, []any{"claudinite-lifecycle", "local/mine"}) {
		t.Errorf("%v", got)
	}
}

// A key the import cannot carry is refused by name, and the report says
// so; nothing is to be written.
func TestRefusals(t *testing.T) {
	for name, c := range map[string]struct{ raw, key string }{
		"slots":          {`{"packs": ["claudinite-tasks"], "taskScheduler": {"dispatch": "slots"}}`, "taskScheduler.dispatch"},
		"other dispatch": {`{"packs": ["claudinite-tasks"], "taskScheduler": {"dispatch": "x"}}`, "taskScheduler.dispatch"},
		"endpoints":      {`{"packs": ["claudinite-tasks"], "taskScheduler": {"endpoints": {}}}`, "taskScheduler.endpoints"},
		"schedule key":   {`{"packs": ["claudinite-tasks"], "taskScheduler": {"cron": "x"}}`, "taskScheduler.cron"},
		"scheduler kind": {`{"taskScheduler": []}`, "taskScheduler"},
		"bad endpoint":   {`{"packs": ["claudinite-tasks"], "taskScheduler": {"agenticTaskInvocationEndpoints": {"a": {"url": "u"}}}}`, "taskScheduler.agenticTaskInvocationEndpoints"},
		"bad disabled":   {`{"packs": ["claudinite-tasks"], "taskScheduler": {"disabledTasks": ["update"]}}`, "taskScheduler.disabledTasks"},
		"no tasks pack":  {`{"packs": ["basics"], "taskScheduler": {"disabledTasks": ["a/b"]}}`, "taskScheduler.disabledTasks"},
		"review no pack": {`{"packs": ["basics"], "dailyClaudiniteUpdatesRequirePrReview": true}`, "dailyClaudiniteUpdatesRequirePrReview"},
		"review kind":    {`{"packs": ["claudinite-tasks"], "dailyClaudiniteUpdatesRequirePrReview": "yes"}`, "dailyClaudiniteUpdatesRequirePrReview"},
		"dormant kind":   {`{"packs": ["claudinite-tasks"], "dormant": 1}`, "dormant"},
		"claudinite":     {`{"claudinite": {}}`, "claudinite"},
		"maintenance":    {`{"maintenance": {}}`, "maintenance"},
		"packConfig":     {`{"packConfig": {}}`, "packConfig"},
		"unknown":        {`{"extra": 1}`, "extra"},
		"packs kind":     {`{"packs": {}}`, "packs"},
		"entry kind":     {`{"packs": [1]}`, "packs[0]"},
		"entry no id":    {`{"packs": [{"config": {}}]}`, "packs[0]"},
		"entry bad id":   {`{"packs": ["Bad Id"]}`, "packs[0]"},
		"local_packs":    {`{"packs": ["local_packs/x"]}`, "packs[0]"},
		"entry key":      {`{"packs": [{"id": "basics", "extra": 1}]}`, "packs[0].extra"},
		"entry config":   {`{"packs": [{"id": "basics", "config": []}]}`, "packs[0].config"},
		"entry rules":    {`{"packs": [{"id": "basics", "rules": []}]}`, "packs[0].rules"},
		"entry accept":   {`{"packs": [{"id": "basics", "accept": {}}]}`, "packs[0].accept"},
		"rules kind":     {`{"rules": []}`, "rules"},
		"accept kind":    {`{"accept": {}}`, "accept"},
	} {
		_, r, err := Read([]byte(c.raw), nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var refused []string
		for _, l := range r {
			if l.Kind == Refused {
				refused = append(refused, l.Key)
			}
		}
		if !r.Refused() || !reflect.DeepEqual(refused, []string{c.key}) {
			t.Errorf("%s: refused %v, want [%s]\n%s", name, refused, c.key, r)
		}
	}
}

func TestNotAnObject(t *testing.T) {
	for _, raw := range []string{`[]`, `{`, `1`, `{} {}`} {
		if _, _, err := Read([]byte(raw), nil); err == nil {
			t.Errorf("%s: accepted", raw)
		}
	}
}

// The import's blocks, spliced into a pin of each format, read back as the
// same declaration, and the pin's bytes and comments survive.
func TestSpliceEveryFormat(t *testing.T) {
	dirs, _ := os.ReadDir(fixtures)
	pins := map[settings.Format]string{
		settings.YAML: "# the pin\nengine:\n  version: \"0.0.0\"\n  # dev\n  manifest: \"" + manifest + "\"\n",
		settings.TOML: "# the pin\n[engine]\nversion = \"0.0.0\"\n# dev\nmanifest = \"" + manifest + "\"\n",
		settings.JSON: "{\n  \"engine\": {\"version\": \"0.0.0\", \"manifest\": \"" + manifest + "\"}\n}\n",
	}
	for _, d := range dirs {
		decl, r := readFixture(t, d.Name())
		if r.Refused() {
			continue
		}
		for f, pin := range pins {
			out, err := settings.SpliceBlocks([]byte(pin), f, decl.Blocks())
			if err != nil {
				t.Errorf("%s %s: %v", d.Name(), f, err)
				continue
			}
			if !strings.HasPrefix(string(out), strings.TrimSuffix(pin, "\n}\n")) {
				t.Errorf("%s %s: the pin changed:\n%s", d.Name(), f, out)
			}
			p, err := settings.ParseFile(out, f)
			if err != nil {
				t.Errorf("%s %s: %v\n%s", d.Name(), f, err, out)
				continue
			}
			if _, err := settings.ReadEngine(out, f); err != nil {
				t.Errorf("%s %s: %v", d.Name(), f, err)
			}
			var tokens []string
			for _, e := range p.Packs.Entries {
				tokens = append(tokens, e.Token())
			}
			var want []string
			for _, e := range settings.Plain(decl.Packs).(map[string]any)["declared"].([]any) {
				switch x := e.(type) {
				case string:
					want = append(want, x)
				case map[string]any:
					want = append(want, x["id"].(string))
				}
			}
			if !reflect.DeepEqual(tokens, want) {
				t.Errorf("%s %s: read back %v, want %v", d.Name(), f, tokens, want)
			}
			back, err := settings.ParseBytesTop(out, f)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := normalize(back["packs"]), normalize(settings.Plain(decl.Packs)); !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: packs read back differ:\n%v\n%v", d.Name(), f, got, want)
			}
		}
	}
}

const manifest = "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="

// normalize sorts nothing and maps numbers to float64, as both parsers
// return them.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalize(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return v
}

// The report names the keys it refused in a stable order.
func TestReportOrderIsTheFile(t *testing.T) {
	_, r, _ := Read([]byte(`{"zeta": 1, "alpha": 2}`), nil)
	var keys []string
	for _, l := range r {
		keys = append(keys, l.Key)
	}
	if sort.StringsAreSorted(keys) {
		t.Errorf("sorted, not in file order: %v", keys)
	}
}
