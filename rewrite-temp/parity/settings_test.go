package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The settings face: what each engine makes of one Node declaration. A
// fixture is testdata/settings/<name>/ holding settings.json (the
// .claudinite-settings.json under test), an optional tree/ (the member
// files it sits beside: local packs, mostly) and expect.json:
//
//	expect      the Node answer, written by CLAUDINITE_PARITY_RECORD=1
//	            through testdata/shims/settings.mjs: the active packs in
//	            declared order, each pack's config, the effective rule
//	            overrides and acceptances, the scheduler's reading, the
//	            blocking config errors and the legacy-shape-in-use
//	            advisories
//	accounts    for each Node error and advisory, the text that answers
//	            it on the cn side: an error is answered by an import
//	            refusal or a verify break, an advisory by an import line
//	            or a verify deprecation
//	divergence  "record-<row>" where cn decides otherwise on purpose, with
//	cn          cn's own answer for the fields it changes, and the
//	            "breaks" it raises that Node has no error for
//
// The cn side is `fromnode import` over the tree beside a development
// pin, then `cn verify` over the imported member.

// SettingsExpect is the Node engine's answer for one declaration.
type SettingsExpect struct {
	Active          []string            `json:"active"`
	Config          map[string]any      `json:"config"`
	SharedConstants []any               `json:"sharedConstants"`
	Rules           map[string]string   `json:"rules"`
	Accept          []map[string]string `json:"accept"`
	Scheduler       SettingsScheduler   `json:"scheduler"`
	Errors          []string            `json:"errors"`
	Advisories      []string            `json:"advisories"`
	Installed       map[string]any      `json:"installed,omitempty"`
}

// SettingsScheduler is the scheduler's reading of a declaration.
type SettingsScheduler struct {
	DisabledTasks []string       `json:"disabledTasks"`
	Endpoints     map[string]any `json:"endpoints"`
	Dormant       bool           `json:"dormant"`
	RequireReview bool           `json:"requireReview"`
}

// SettingsFixture is one settings-face fixture.
type SettingsFixture struct {
	Name, Dir  string
	Expect     json.RawMessage   `json:"expect"`
	Accounts   map[string]string `json:"accounts"`
	Divergence string            `json:"divergence,omitempty"`
	Cn         json.RawMessage   `json:"cn,omitempty"`
}

// LoadSettingsFixtures reads every fixture under root, sorted by name.
func LoadSettingsFixtures(root string) ([]SettingsFixture, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []SettingsFixture
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		f := SettingsFixture{Name: e.Name(), Dir: dir}
		if !exists(filepath.Join(dir, "settings.json")) {
			return nil, fmt.Errorf("%s: no settings.json", dir)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "expect.json"))
		if err == nil {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&f); err != nil {
				return nil, fmt.Errorf("%s/expect.json: %w", dir, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if (f.Divergence == "") != (len(f.Cn) == 0) {
			return nil, fmt.Errorf("%s: a divergence names its record row and carries cn's answer, both or neither", dir)
		}
		if f.Divergence != "" && !divergenceForm.MatchString(f.Divergence) {
			return nil, fmt.Errorf("%s: divergence %q is not record-<row>", dir, f.Divergence)
		}
		f.Name, f.Dir = e.Name(), dir
		out = append(out, f)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out, nil
}

// nodeSettings answers a fixture with the frozen engine.
func nodeSettings(t *testing.T, e Node, f SettingsFixture) json.RawMessage {
	t.Helper()
	dir := memberDir(t)
	if err := layFixture(f, dir); err != nil {
		t.Fatal(err)
	}
	if err := gitDo(dir, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := commitAll(dir, "fixture"); err != nil {
		t.Fatal(err)
	}
	shim, err := filepath.Abs(filepath.Join("testdata", "shims", "settings.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code, err := run(dir, []string{nodeEnv + "=" + e.Root}, "", "node", shim, dir)
	if err != nil || code != 0 {
		t.Fatalf("node settings shim: exit %d %v: %s", code, err, stderr)
	}
	return json.RawMessage(stdout)
}

func layFixture(f SettingsFixture, dir string) error {
	if tree := filepath.Join(f.Dir, "tree"); exists(tree) {
		if err := copyTree(tree, dir); err != nil {
			return err
		}
	}
	raw, err := os.ReadFile(filepath.Join(f.Dir, "settings.json"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".claudinite-settings.json"), raw, 0o644)
}

// cnShape is the member every cn side starts from: a whole member of the
// current shape, its declaration and vendored packs removed.
const cnShape = "v7-update-as-task"

// CnSettingsAnswer is what the cn side produced.
type CnSettingsAnswer struct {
	Code     int
	Report   []string
	Verify   []string
	Imported map[string]any
}

// cnImport answers a fixture with cn: the import beside a development
// pin written as JSON, then verify over the imported member.
func cnImport(t *testing.T, c Cn, f SettingsFixture, nodeRoot string) CnSettingsAnswer {
	t.Helper()
	dir := memberDir(t)
	if err := copyDir(filepath.Join("../..", "cn", "packaging", "verify", "testdata", "shapes", cnShape), dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".claudinite/settings.yaml", ".claudinite/shared"} {
		if err := os.RemoveAll(filepath.Join(dir, p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := layFixture(f, dir); err != nil {
		t.Fatal(err)
	}
	pin := fmt.Sprintf("{\n  \"engine\": {\"version\": \"0.0.0\", \"manifest\": %q}\n}\n", DevManifest)
	if err := os.WriteFile(filepath.Join(dir, ".claudinite/settings.json"), []byte(pin), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code, err := run(dir, c.env(dir), "", c.FromNode, "import", "--repo", dir)
	if err != nil || code > 1 {
		t.Fatalf("fromnode import: exit %d %v: %s%s", code, err, stdout, stderr)
	}
	a := CnSettingsAnswer{Code: code, Report: nonEmpty(stdout)}
	raw, err := os.ReadFile(filepath.Join(dir, ".claudinite/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		if err := json.Unmarshal(raw, &a.Imported); err != nil {
			t.Fatalf("the imported settings are not JSON: %v\n%s", err, raw)
		}
		for _, id := range importedCanon(a.Imported) {
			if err := VendorPack(id, filepath.Join(nodeRoot, "packs"), "cn", filepath.Join(dir, ".claudinite/shared/packs", id)); err != nil {
				t.Fatal(err)
			}
		}
	} else if string(raw) != pin {
		t.Errorf("a refused import changed the settings file:\n%s", raw)
	}
	out, stderr, vcode, err := run(dir, c.env(dir), "", c.Binary, "verify", "--repo", dir)
	if err != nil || vcode > 1 {
		t.Fatalf("cn verify: exit %d %v: %s", vcode, err, stderr)
	}
	for _, l := range nonEmpty(out) {
		if cnLine.MatchString(l) {
			a.Verify = append(a.Verify, l)
		}
	}
	return a
}

func nonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func declaredOfImported(imported map[string]any) []any {
	packs, _ := imported["packs"].(map[string]any)
	list, _ := packs["declared"].([]any)
	return list
}

func entryToken(e any) (string, map[string]any) {
	switch v := e.(type) {
	case string:
		return v, nil
	case map[string]any:
		id, _ := v["id"].(string)
		return id, v
	}
	return "", nil
}

func importedCanon(imported map[string]any) []string {
	var out []string
	for _, e := range declaredOfImported(imported) {
		if tok, _ := entryToken(e); tok != "" && !strings.HasPrefix(tok, "local/") {
			out = append(out, tok)
		}
	}
	return out
}

// settingsRules are the verify rules that judge the declaration and the
// member's own packs: a break from one of them, where Node reports no
// error, is a divergence the fixture must name.
var settingsRules = map[string]bool{
	"settings-file": true, "settings-checks": true, "pack-declared": true,
	"descriptor-format": true, "descriptor-duplicate": true, "node-leftovers": true,
}

func packRel(token string) string {
	if name, ok := strings.CutPrefix(token, "local/"); ok {
		return ".claudinite/local/packs/" + name
	}
	return ".claudinite/shared/packs/" + token
}

// cnView reads the imported declaration as the Node answer's fields.
func (a CnSettingsAnswer) cnView(node SettingsExpect) map[string]any {
	broken := map[string]bool{}
	for _, l := range a.Verify {
		m := cnLine.FindStringSubmatch(l)
		if m[1] == "break" && (m[2] == "pack-declared" || m[2] == "descriptor-format") {
			broken[m[3]] = true
		}
	}
	view := map[string]any{}
	var active []any
	config := map[string]any{}
	rules := map[string]any{}
	var accept []any
	sched := map[string]any{"disabledTasks": []any{}, "endpoints": map[string]any{}, "dormant": false, "requireReview": false}
	var shared []any
	checks, _ := a.Imported["checks"].(map[string]any)
	if r, ok := checks["rules"].(map[string]any); ok {
		for k, v := range r {
			rules[k] = v
		}
	}
	if acc, ok := checks["accept"].([]any); ok {
		accept = append(accept, acc...)
	}
	for _, e := range declaredOfImported(a.Imported) {
		tok, obj := entryToken(e)
		rel := packRel(tok)
		load := true
		for b := range broken {
			load = load && b != rel && !strings.HasPrefix(b, rel+"/")
		}
		if load {
			active = append(active, tok)
		}
		bare := strings.TrimPrefix(tok, "local/")
		if c, ok := obj["config"].(map[string]any); ok {
			c = copyMap(c)
			if tok == "claudinite-tasks" {
				nodeOwn, _ := node.Config["claudinite-tasks"].(map[string]any)
				for k, to := range map[string]string{"disabledTasks": "disabledTasks", "agenticTaskInvocationEndpoints": "endpoints", "dormant": "dormant", "dailyClaudiniteUpdatesRequirePrReview": "requireReview"} {
					if v, ok := c[k]; ok {
						if to == "dormant" || to == "requireReview" {
							sched[to] = v == true
						} else {
							sched[to] = v
						}
						if _, own := nodeOwn[k]; !own {
							delete(c, k)
						}
					}
				}
			}
			if tok == "basics" {
				if sc, ok := c["sharedConstants"].([]any); ok {
					shared = sc
				}
				nodeOwn, _ := node.Config["basics"].(map[string]any)
				if _, own := nodeOwn["sharedConstants"]; !own {
					delete(c, "sharedConstants")
				}
			}
			if len(c) > 0 {
				config[tok] = c
			}
		}
		if r, ok := obj["rules"].(map[string]any); ok {
			for k, v := range r {
				rules[k] = v
			}
		}
		if acc, ok := obj["accept"].([]any); ok {
			for _, x := range acc {
				m := copyMap(x.(map[string]any))
				m["pack"] = bare
				accept = append(accept, m)
			}
		}
	}
	if active == nil {
		active = []any{}
	}
	if accept == nil {
		accept = []any{}
	}
	if shared == nil {
		shared = []any{}
	}
	view["active"], view["config"], view["rules"], view["accept"], view["scheduler"], view["sharedConstants"] = active, config, rules, accept, sched, shared
	return view
}

func copyMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// comparedFields are the fields both engines must agree on.
var comparedFields = []string{"active", "config", "sharedConstants", "rules", "accept", "scheduler"}

func fieldsOf(raw json.RawMessage) (map[string]any, error) {
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, k := range comparedFields {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

var reportLine = regexp.MustCompile(`^(mapped|carried|dropped|refused) `)

func TestParitySettings(t *testing.T) {
	fixtures, err := LoadSettingsFixtures(filepath.Join("testdata", "settings"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 14 {
		t.Fatalf("%d settings fixtures; the face holds at least 14", len(fixtures))
	}
	recording := os.Getenv(recordEnv) == "1"
	var root string
	for _, e := range engines(t) {
		if n, ok := e.(Node); ok {
			root = n.Root
		}
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			for _, e := range engines(t) {
				switch e := e.(type) {
				case Node:
					got := nodeSettings(t, e, f)
					if recording {
						if err := recordSettings(f, got); err != nil {
							t.Fatal(err)
						}
						continue
					}
					if len(f.Expect) == 0 {
						t.Fatalf("%s has no expect; record it against the Node engine first (%s=1)", f.Dir, recordEnv)
					}
					var g, w any
					_ = json.Unmarshal(got, &g)
					_ = json.Unmarshal(f.Expect, &w)
					if !reflect.DeepEqual(g, w) {
						t.Errorf("node disagrees with expect:\n%s", strings.Join(jsonDiff("$", g, w, 30), "\n"))
					}
				case Cn:
					if recording {
						continue
					}
					if len(f.Expect) == 0 {
						t.Fatalf("%s has no expect; record it against the Node engine first (%s=1)", f.Dir, recordEnv)
					}
					if root == "" {
						root = os.Getenv(nodeEnv)
					}
					if root == "" {
						t.Skip(nodeEnv + " is not set; the cn side vendors the frozen shelf")
					}
					checkCnSettings(t, f, cnImport(t, e, f, root))
				}
			}
		})
	}
}

func checkCnSettings(t *testing.T, f SettingsFixture, a CnSettingsAnswer) {
	t.Helper()
	var node SettingsExpect
	if err := json.Unmarshal(f.Expect, &node); err != nil {
		t.Fatal(err)
	}
	var cnWant struct {
		Breaks []string `json:"breaks"`
	}
	if f.Divergence != "" {
		if err := json.Unmarshal(f.Cn, &cnWant); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		if t.Failed() {
			t.Logf("import (exit %d):\n%s\nverify:\n%s", a.Code, strings.Join(a.Report, "\n"), strings.Join(a.Verify, "\n"))
		}
	}()
	for _, l := range a.Report {
		if !reportLine.MatchString(l) {
			t.Errorf("the import printed %q, which is not a report line", l)
		}
	}
	lines := map[string][]string{}
	for _, l := range a.Report {
		lines["report"] = append(lines["report"], l)
		if strings.HasPrefix(l, "refused ") {
			lines["refusal"] = append(lines["refusal"], l)
		}
	}
	for _, l := range a.Verify {
		m := cnLine.FindStringSubmatch(l)
		lines[m[1]] = append(lines[m[1]], l)
	}
	answered := func(want string, classes ...string) bool {
		for _, c := range classes {
			for _, l := range lines[c] {
				if strings.Contains(l, want) {
					return true
				}
			}
		}
		return false
	}
	anyClass := []string{"report", "break", "deprecation"}
	for _, what := range node.Errors {
		want, ok := f.Accounts[what]
		switch {
		case !ok:
			t.Errorf("node error %q has no account in expect.json", what)
		case f.Divergence == "" && !answered(want, "refusal", "break"):
			t.Errorf("node error %q: no import refusal or verify break holds %q", what, want)
		case !answered(want, anyClass...):
			t.Errorf("node error %q: nothing on the cn side holds %q", what, want)
		}
	}
	for _, what := range node.Advisories {
		want, ok := f.Accounts[what]
		switch {
		case !ok:
			t.Errorf("node advisory %q has no account in expect.json", what)
		case f.Divergence == "" && !answered(want, "report", "deprecation"):
			t.Errorf("node advisory %q: no import line or verify deprecation holds %q", what, want)
		case !answered(want, anyClass...):
			t.Errorf("node advisory %q: nothing on the cn side holds %q", what, want)
		}
	}
	for what := range f.Accounts {
		if !contains(node.Errors, what) && !contains(node.Advisories, what) {
			t.Errorf("expect.json accounts for %q, which Node no longer says", what)
		}
	}
	for _, want := range cnWant.Breaks {
		if !answered(want, "break") {
			t.Errorf("cn should break with %q (%s), and does not", want, f.Divergence)
		}
	}
	if len(node.Errors) == 0 {
		if a.Code != 0 {
			t.Errorf("node reads the declaration with no error, and the import refuses it:\n%s", strings.Join(a.Report, "\n"))
		}
		for _, l := range lines["break"] {
			m := cnLine.FindStringSubmatch(l)
			named := false
			for _, want := range cnWant.Breaks {
				named = named || strings.Contains(l, want)
			}
			if settingsRules[m[2]] && !named {
				t.Errorf("node reads the declaration with no error, and verify breaks: %s", l)
			}
		}
	}
	if a.Code != 0 {
		if len(node.Errors) == 0 {
			return
		}
		t.Logf("refused, as Node errs: %s", strings.Join(lines["refusal"], "; "))
		return
	}
	want, err := fieldsOf(f.Expect)
	if err != nil {
		t.Fatal(err)
	}
	if f.Divergence != "" {
		over, err := fieldsOf(f.Cn)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range over {
			want[k] = v
		}
	}
	var got map[string]any
	_ = json.Unmarshal(mustJSON(a.cnView(node)), &got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cn disagrees with %s:\n%s\nreport:\n%s", map[bool]string{true: "cn (" + f.Divergence + ")", false: "expect"}[f.Divergence != ""],
			strings.Join(jsonDiff("$", got, want, 30), "\n"), strings.Join(a.Report, "\n"))
	}
	if f.Divergence != "" {
		nodeFields, _ := fieldsOf(f.Expect)
		if reflect.DeepEqual(got, nodeFields) && len(cnWant.Breaks) == 0 && len(node.Errors) == 0 {
			t.Errorf("%s is marked %s but cn now gives the Node answer: drop the divergence", f.Dir, f.Divergence)
		}
	}
}

func recordSettings(f SettingsFixture, answer json.RawMessage) error {
	var v any
	if err := json.Unmarshal(answer, &v); err != nil {
		return fmt.Errorf("the shim printed no JSON: %v\n%s", err, answer)
	}
	body := map[string]json.RawMessage{"expect": mustJSON(v)}
	if len(f.Accounts) > 0 {
		body["accounts"] = mustJSON(f.Accounts)
	}
	if f.Divergence != "" {
		body["divergence"] = mustJSON(f.Divergence)
		body["cn"] = f.Cn
	}
	var b bytes.Buffer
	b.WriteString("{")
	first := true
	for _, k := range []string{"expect", "accounts", "divergence", "cn"} {
		raw, ok := body[k]
		if !ok {
			continue
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "%q:%s", k, raw)
	}
	b.WriteString("}")
	var out bytes.Buffer
	if err := json.Indent(&out, b.Bytes(), "", "  "); err != nil {
		return err
	}
	out.WriteString("\n")
	return os.WriteFile(filepath.Join(f.Dir, "expect.json"), out.Bytes(), 0o644)
}

// The parity README counts the settings face's divergences; the count
// must be the fixtures' own.
func TestSettingsDivergencesAreCounted(t *testing.T) {
	fixtures, err := LoadSettingsFixtures(filepath.Join("testdata", "settings"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range fixtures {
		if f.Divergence != "" {
			n++
		}
	}
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^Settings face divergences: (\d+) of (\d+) fixtures\.$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("parity/README.md has no line `Settings face divergences: <n> of <total> fixtures.`")
	}
	if want := fmt.Sprintf("%d of %d", n, len(fixtures)); m[1]+" of "+m[2] != want {
		t.Errorf("parity/README.md counts %s of %s divergences, the fixtures mark %s", m[1], m[2], want)
	}
}
