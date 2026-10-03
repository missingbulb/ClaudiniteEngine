package fleet_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// world is a fake GitHub: a GET answers a file's contents, a directory's
// listing or a raw response by path; anything else answers 404.
type world struct {
	files map[string]string
	raw   map[string]fleet.Response
	calls []string
}

func (w *world) gh(method, path string, _ any) (fleet.Response, error) {
	w.calls = append(w.calls, method+" "+path)
	if r, ok := w.raw[path]; ok {
		return r, nil
	}
	if method != "GET" {
		return fleet.Response{Status: 404}, nil
	}
	const prefix = "/repos/acme/m/contents/"
	rel, ok := strings.CutPrefix(path, prefix)
	if !ok {
		return fleet.Response{Status: 404}, nil
	}
	if text, ok := w.files[rel]; ok {
		raw, _ := json.Marshal(map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(text)), "sha": "s"})
		return fleet.Response{Status: 200, JSON: raw}, nil
	}
	var dir []map[string]string
	for p := range w.files {
		if rest, ok := strings.CutPrefix(p, rel+"/"); ok && !strings.Contains(rest, "/") {
			dir = append(dir, map[string]string{"name": rest, "type": "file"})
		}
	}
	if dir != nil {
		raw, _ := json.Marshal(dir)
		return fleet.Response{Status: 200, JSON: raw}, nil
	}
	return fleet.Response{Status: 404}, nil
}

const manifest = "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="

var declarations = map[settings.Format]string{
	settings.YAML: "engine:\n  version: \"61001.1.0\"\n  manifest: \"" + manifest + "\"\npacks:\n  declared:\n    - basics\n    - id: claudinite-tasks\n      config:\n        dormant: true\n",
	settings.TOML: "[engine]\nversion = \"61001.1.0\"\nmanifest = \"" + manifest + "\"\n\n[packs]\ndeclared = [\"basics\"]\n",
	settings.JSON: "{\"engine\": {\"version\": \"61001.1.0\", \"manifest\": \"" + manifest + "\"}, \"packs\": {\"declared\": [\"basics\"]}}\n",
}

func TestReadMemberReadsEachFormat(t *testing.T) {
	for f, text := range declarations {
		w := &world{files: map[string]string{
			".claudinite/settings." + string(f):         text,
			".claudinite/shared/packs/basics/pack.json": `{"id": "basics", "version": "3.1.0"}`,
		}}
		m, err := fleet.ReadMember(w.gh, "acme/m", "main")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if m.Shape != fleet.ShapeCn || m.Format != f || m.Pin.Version != "61001.1.0" || m.Held["basics"] != "3.1.0" || !m.Covered() {
			t.Errorf("%s: %+v", f, m)
		}
		if m.SettingsPath() != ".claudinite/settings."+string(f) {
			t.Errorf("%s: path %s", f, m.SettingsPath())
		}
		if dormant := f == settings.YAML; m.Dormant != dormant {
			t.Errorf("%s: dormant %v", f, m.Dormant)
		}
	}
}

func TestTwoSettingsFilesAreAnErrorNeverUncovered(t *testing.T) {
	w := &world{files: map[string]string{
		".claudinite/settings.yaml": declarations[settings.YAML],
		".claudinite/settings.json": declarations[settings.JSON],
	}}
	if _, err := fleet.ReadMember(w.gh, "acme/m", "main"); err == nil || !strings.Contains(err.Error(), "carries 2 settings files") {
		t.Fatalf("err %v", err)
	}
}

func TestANodeTreeIsCoveredAndANeitherTreeIsNot(t *testing.T) {
	w := &world{files: map[string]string{".claudinite-settings.json": `{"packs": ["basics"]}`}}
	m, err := fleet.ReadMember(w.gh, "acme/m", "main")
	if err != nil || m.Shape != fleet.ShapeNode || !m.Covered() || m.SettingsPath() != fleet.NodeSettingsFile {
		t.Fatalf("%+v %v", m, err)
	}
	if fleet.Classify(fleet.FreshIn{Shape: m.Shape}).State != fleet.StateNode {
		t.Error("a node member is measured")
	}
	w = &world{files: map[string]string{"README.md": "hi"}}
	m, err = fleet.ReadMember(w.gh, "acme/m", "main")
	if err != nil || m.Shape != fleet.ShapeNone || m.Covered() {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestAnUnparsableDeclarationIsAnError(t *testing.T) {
	w := &world{files: map[string]string{".claudinite/settings.json": "{not json"}}
	if _, err := fleet.ReadMember(w.gh, "acme/m", "main"); err == nil || !strings.Contains(err.Error(), "unparsable .claudinite/settings.json") {
		t.Fatalf("err %v", err)
	}
}

func TestAForbiddenContentsReadIsAGrantErrorNamingThePermission(t *testing.T) {
	w := &world{raw: map[string]fleet.Response{"/repos/acme/m/contents/.claudinite": {Status: 403}}}
	_, err := fleet.ReadMember(w.gh, "acme/m", "main")
	if !fleet.IsGrant(err) || !strings.Contains(err.Error(), fleet.ForbiddenHint("/repos/acme/m/contents/")) {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(fleet.ForbiddenHint("/repos/acme/m/contents/"), "Contents") {
		t.Errorf("hint %q names no Contents permission", fleet.ForbiddenHint("/repos/acme/m/contents/"))
	}
	var g *fleet.GrantError
	if !errors.As(err, &g) {
		t.Fatal("not a GrantError")
	}
}

// shelf offers one engine version and one pack index.
type shelf struct {
	engine string
	index  map[string]packindex.Index
	err    error
}

func (s shelf) Packument(string) (*npmreg.Packument, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &npmreg.Packument{Versions: map[string]npmreg.Version{s.engine: {Version: s.engine}}}, nil
}

func (s shelf) Index(id string) (packindex.Index, bool, error) {
	ix, ok := s.index[id]
	return ix, ok, nil
}

func TestMeasureIsWhatTheMembersOwnUpdateWouldDecide(t *testing.T) {
	w := &world{files: map[string]string{
		".claudinite/settings.toml":                 declarations[settings.TOML],
		".claudinite/shared/packs/basics/pack.json": `{"version": "3.1.0"}`,
	}}
	m, err := fleet.ReadMember(w.gh, "acme/m", "main")
	if err != nil {
		t.Fatal(err)
	}
	s := shelf{engine: "61002.1.0", index: map[string]packindex.Index{"basics": {Pack: "basics",
		Versions: []packindex.Entry{{Version: "3.2.0", Channel: "stable", MinEngineVersion: "60000.1"}}}}}
	in, err := fleet.Measure(m, true, s)
	if err != nil {
		t.Fatal(err)
	}
	got := fleet.Classify(in)
	if got.State != fleet.StateBehind || got.Detail != "behind the published versions by engine 61001.1.0 → 61002.1.0, basics 3.1.0 → 3.2.0" {
		t.Errorf("%+v", got)
	}
	s = shelf{engine: "61001.1.0", index: map[string]packindex.Index{}}
	in, _ = fleet.Measure(m, true, s)
	if got := fleet.Classify(in); got.State != fleet.StateFresh || got.Detail != "engine 61001.1.0, 1 declared pack(s) at the published versions" {
		t.Errorf("a pack the shelf does not offer is no gap: %+v", got)
	}
	if got := fleet.Classify(fleet.FreshIn{Shape: fleet.ShapeCn, Format: settings.TOML, Pin: "61001.1.0"}); got.State != fleet.StateNoScheduler {
		t.Errorf("no scheduler: %+v", got)
	}
	if _, err := fleet.Measure(m, true, shelf{err: errors.New("offline")}); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Errorf("an unreadable shelf is unknown, got %v", err)
	}
	w.calls = nil
	if _, err := fleet.Measure(m, false, shelf{err: errors.New("offline")}); err != nil {
		t.Errorf("a member with no scheduler needs no shelf read: %v", err)
	}
}

func TestEnumerateRefusesAnOwnerWithNothing(t *testing.T) {
	w := &world{raw: map[string]fleet.Response{"/user/repos?affiliation=owner&per_page=100&page=1": {Status: 200,
		JSON: json.RawMessage(`[{"name":"m","full_name":"other/m","owner":{"login":"other"}}]`)}}}
	_, err := fleet.Enumerate(w.gh, "Acme")
	if !errors.Is(err, fleet.ErrNoOwnedRepos) {
		t.Fatalf("err %v", err)
	}
	w.raw["/user/repos?affiliation=owner&per_page=100&page=1"] = fleet.Response{Status: 403}
	if _, err := fleet.Enumerate(w.gh, "acme"); !fleet.IsGrant(err) {
		t.Fatalf("a 403 enumeration: %v", err)
	}
}

func TestACnMemberSignalsItsPacksAndItsScheduler(t *testing.T) {
	w := &world{
		files: map[string]string{
			".claudinite/settings.json":                  `{"engine": {"version": "61001.1.0", "manifest": "` + manifest + `"}, "packs": {"declared": ["basics", {"id": "local/mine", "config": {"a": 1}}]}}`,
			".github/workflows/claudinite-scheduler.yml": "on: {}\n",
		},
		raw: map[string]fleet.Response{"/user/repos?affiliation=owner&per_page=100&page=1": {Status: 200,
			JSON: json.RawMessage(`[{"name":"m","full_name":"acme/m","owner":{"login":"acme"},"default_branch":"main"}]`)}},
	}
	s := fleet.ReadFleet(w.gh, "Acme", "2026-10-01T00:00:00Z")
	if s.Error != "" || len(s.Members) != 1 {
		t.Fatalf("%+v", s)
	}
	m := s.Members[0]
	if strings.Join(m.ActivePacks, ",") != "basics,mine" || !m.SchedulesItself || m.Stamp != nil || m.PackConfigs["mine"] == nil {
		t.Errorf("%+v", m)
	}
}

// A repository outside the fleet's owner is out of scope before its tree
// is read, so judging one reads nothing of it.
func TestARepositoryOutsideTheOwnerIsJudgedWithoutAnyRead(t *testing.T) {
	w := &world{}
	r := fleet.Repo{Name: "x", FullName: "Other/x"}
	r.Owner.Login = "Other"
	v := fleet.JudgeRepo(w.gh, r, "acme/manager", fleet.Config{Owner: "acme"}, nil)
	if v.Scope != fleet.ScopeOutOfOwner || len(w.calls) != 0 {
		t.Fatalf("scope %q after %v", v.Scope, w.calls)
	}
}

// An offset instant carries a "+", which a query reads as a space: the
// window's since is escaped.
func TestTheSignalWindowIsEscapedInTheCommitsQuery(t *testing.T) {
	w := &world{
		files: map[string]string{".claudinite/settings.json": `{"packs": {"declared": ["basics"]}}`},
		raw: map[string]fleet.Response{"/user/repos?affiliation=owner&per_page=100&page=1": {Status: 200,
			JSON: json.RawMessage(`[{"name":"m","full_name":"acme/m","owner":{"login":"acme"},"default_branch":"main"}]`)}},
	}
	fleet.ReadFleet(w.gh, "acme", "2026-10-01T00:00:00+03:00")
	want := "GET /repos/acme/m/commits?sha=main&since=2026-10-01T00%3A00%3A00%2B03%3A00&per_page=100&page=1"
	for _, c := range w.calls {
		if c == want {
			return
		}
	}
	t.Fatalf("no %q among %v", want, w.calls)
}
