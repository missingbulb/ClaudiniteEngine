package seeds_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/mirror"
	"github.com/missingbulb/ClaudiniteEngine/fleet/seeds"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// tree fakes the contents API over each repo's files, recording every
// write's new text.
type tree struct {
	files map[string]string // "owner/name:path" -> text
	puts  map[string]string
}

func (tr *tree) gh(method, path string, body any) (fleet.Response, error) {
	rest, ok := strings.CutPrefix(path, "/repos/")
	if !ok {
		return fleet.Response{Status: 500}, nil
	}
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 4 || parts[2] != "contents" {
		return fleet.Response{Status: 500}, nil
	}
	key := parts[0] + "/" + parts[1] + ":" + parts[3]
	if method == "PUT" {
		raw, _ := base64.StdEncoding.DecodeString(body.(map[string]string)["content"])
		if tr.puts == nil {
			tr.puts = map[string]string{}
		}
		tr.puts[key] = string(raw)
		return fleet.Response{Status: 200}, nil
	}
	if text, ok := tr.files[key]; ok {
		raw, _ := json.Marshal(map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(text)), "sha": "s"})
		return fleet.Response{Status: 200, JSON: raw}, nil
	}
	dir := key + "/"
	var entries []map[string]string
	for k := range tr.files {
		if name, ok := strings.CutPrefix(k, dir); ok && !strings.Contains(name, "/") {
			entries = append(entries, map[string]string{"name": name, "type": "file"})
		}
	}
	if entries != nil {
		raw, _ := json.Marshal(entries)
		return fleet.Response{Status: 200, JSON: raw}, nil
	}
	return fleet.Response{Status: 404}, nil
}

func settingsAt(pin string) string {
	return "engine:\n  version: \"" + pin + "\"\n  manifest: \"sha512-" + strings.Repeat("A", 86) + "==\"\npacks:\n  declared:\n    - basics\n"
}

func repo(full string) fleet.Repo {
	r := fleet.Repo{FullName: full, DefaultBranch: "main"}
	r.Owner.Login = strings.Split(full, "/")[0]
	return r
}

func cfg() fleet.Config {
	c, err := fleet.ParseConfig(map[string]any{"owner": "acme"}, true, "acme/fleet")
	if err != nil {
		panic(err)
	}
	return c
}

func mirrored(declared *[]string) seeds.MirrorFunc {
	return func(ids []string) (mirror.Result, error) {
		*declared = ids
		return mirror.Result{Packs: 3}, nil
	}
}

// Every member the fleet reaches reads its manager alone, written once
// and never over a choice it already made; the manager reads the shelf.
func TestEveryMemberIsPointedAtItsManager(t *testing.T) {
	tr := &tree{files: map[string]string{
		"acme/fleet:.claudinite/settings.yaml": settingsAt("1.61006.1"),
		"acme/new:.claudinite/settings.yaml":   settingsAt("1.61006.1"),
		"acme/own:.claudinite/settings.yaml":   settingsAt("1.61006.1") + "  sources:\n    - \"https://packs.example.com\"\n",
		"acme/old:.claudinite/settings.yaml":   settingsAt("1.61005.1"),
	}}
	var declared []string
	r := seeds.Sweep(tr.gh, []fleet.Repo{repo("acme/fleet"), repo("acme/new"), repo("acme/own"), repo("acme/old")}, "acme/fleet", cfg(), mirrored(&declared))
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(declared, ",") != "basics" {
		t.Errorf("mirrored the members' packs %v", declared)
	}
	if len(tr.puts) != 1 {
		t.Fatalf("wrote %v", tr.puts)
	}
	got := tr.puts["acme/new:.claudinite/settings.yaml"]
	p, err := settings.ReadPacks([]byte(got), settings.YAML)
	if err != nil || len(p.Sources) != 1 || p.Sources[0] != "acme/fleet" || strings.Join(p.Declared, ",") != "basics" {
		t.Errorf("%+v %v\n%s", p, err, got)
	}
	out := r.Render()
	for _, want := range []string{"acme/new", "acme/own", "acme/old — pinned to 1.61005.1", "Mirror: acme/fleet's vendored branch holds 3 packs"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}

// A mirror that failed points no member at a branch it could not write,
// and fails the run.
func TestAFailedMirrorPointsNoMember(t *testing.T) {
	tr := &tree{files: map[string]string{"acme/new:.claudinite/settings.yaml": settingsAt("1.61006.1")}}
	fail := func([]string) (mirror.Result, error) {
		return mirror.Result{}, &fleet.GrantError{Msg: "POST blobs returned 403"}
	}
	r := seeds.Sweep(tr.gh, []fleet.Repo{repo("acme/new")}, "acme/fleet", cfg(), fail)
	if len(tr.puts) != 0 {
		t.Errorf("wrote %v", tr.puts)
	}
	if err := r.Err(); err == nil || !fleet.IsGrant(err) {
		t.Errorf("%v", err)
	}
	other := func([]string) (mirror.Result, error) { return mirror.Result{}, errors.New("shelf down") }
	if err := seeds.Sweep(tr.gh, []fleet.Repo{repo("acme/new")}, "acme/fleet", cfg(), other).Err(); err == nil {
		t.Error("a failed mirror passed")
	}
}

// A seed and the sources land in one write.
func TestASeedAndTheSourcesLandTogether(t *testing.T) {
	tr := &tree{files: map[string]string{
		"acme/new:.claudinite/settings.yaml":                    settingsAt("1.61006.1"),
		"acme/new:.claudinite/shared/packs/acme-pack/pack.json": `{"version": "1.61005.1"}`,
	}}
	c := cfg()
	c.PackSeeds = []fleet.Seed{{ID: "acme-pack"}}
	var declared []string
	r := seeds.Sweep(tr.gh, []fleet.Repo{repo("acme/new")}, "acme/fleet", c, mirrored(&declared))
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	p, err := settings.ReadPacks([]byte(tr.puts["acme/new:.claudinite/settings.yaml"]), settings.YAML)
	if err != nil || strings.Join(p.Declared, ",") != "basics,acme-pack" || len(p.Sources) != 1 {
		t.Errorf("%+v %v", p, err)
	}
	if strings.Join(declared, ",") != "acme-pack,basics" {
		t.Errorf("mirrored %v; a seed is a pack the fleet will carry", declared)
	}
}
