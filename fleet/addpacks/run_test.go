package addpacks_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/addpacks"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
)

var shelf = []packindex.CatalogPack{{ID: "acme-pack", Version: "1.0.0", Channel: packindex.Stable, Requires: []string{}}}

// recorder answers acme/good as a covered Node member and every other
// read with a 404, and keeps each call that is not a read.
func recorder(writes *[]string) fleet.GH {
	return func(method, path string, _ any) (fleet.Response, error) {
		if method != "GET" {
			*writes = append(*writes, method+" "+path)
			return fleet.Response{Status: 201, JSON: json.RawMessage(`{"number": 1}`)}, nil
		}
		if path == "/repos/acme/good/contents/.claudinite-settings.json" {
			body, _ := json.Marshal(map[string]string{
				"content": base64.StdEncoding.EncodeToString([]byte(`{"packs": ["basics"]}`)),
				"sha":     "abc",
			})
			return fleet.Response{Status: 200, JSON: body}, nil
		}
		return fleet.Response{Status: 404, JSON: json.RawMessage(`{"message": "Not Found"}`)}, nil
	}
}

// A force naming one repo it cannot place the request in is refused
// whole: the member it could reach is not written either.
func TestAForceWithOneBadTargetWritesNothing(t *testing.T) {
	var writes []string
	p := addpacks.Params{AddPacks: []string{"acme-pack"}, Repos: []string{"good", "absent"}, Forced: true}
	cfg := fleet.Config{Owner: "acme"}
	if err := addpacks.Validate(p, cfg.Owner, cfg, shelf); err != nil {
		t.Fatalf("validate: %v", err)
	}
	repos := []fleet.Repo{{Name: "good", FullName: "acme/good", DefaultBranch: "main"}}
	_, err := addpacks.Run(recorder(&writes), repos, "acme/manager", cfg, p, addpacks.FixedCorpus(shelf))
	if !addpacks.IsRefusal(err) {
		t.Fatalf("want a refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "acme/absent") {
		t.Errorf("the refusal does not name the bad target: %v", err)
	}
	if strings.Contains(err.Error(), "acme/good") {
		t.Errorf("the reachable member was refused too, so the test proves nothing: %v", err)
	}
	if len(writes) != 0 {
		t.Errorf("a refused force wrote %v", writes)
	}
}

// A force naming a pack the catalog does not carry is refused before any
// member is read.
func TestAForceOfAnUnknownPackIsRefusedUpFront(t *testing.T) {
	p := addpacks.Params{AddPacks: []string{"acme-other"}, Repos: []string{"good"}, Forced: true}
	err := addpacks.Validate(p, "acme", fleet.Config{Owner: "acme"}, shelf)
	if !addpacks.IsRefusal(err) || !strings.Contains(err.Error(), "1-pack catalog") {
		t.Fatalf("got %v", err)
	}
}

// The protocol the member half holds its copy to.
func TestProtocolNamesTheWorkListConstants(t *testing.T) {
	p := addpacks.Protocol()
	for _, k := range []string{"label", "mark", "memberTaskId", "requestedTitle", "suspectedTitle"} {
		if p[k] == "" {
			t.Errorf("protocol has no %s: %v", k, p)
		}
	}
}
