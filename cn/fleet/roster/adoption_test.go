package roster_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/roster"
)

const listing = "/repos/acme/manager/issues?state=all&per_page=100&page=1"

// table answers a listing and one status for every write to an issue.
func table(issues string, write int) fleet.GH {
	return func(method, path string, _ any) (fleet.Response, error) {
		if method == "GET" && path == listing {
			return fleet.Response{Status: 200, JSON: json.RawMessage(issues)}, nil
		}
		return fleet.Response{Status: write}, nil
	}
}

const closedCompleted = `[{"number": 4, "title": "Adopt acme/u into the Claudinite fleet", "state": "closed", "state_reason": "completed", "closed_at": "2026-01-01T00:00:00Z"}]`

const openForCovered = `[{"number": 5, "title": "Adopt acme/c into the Claudinite fleet", "state": "open"}]`

// A reopen or close the API refused is not an action taken: a 403 is the
// token's grant, parked action with the Issues hint, and any other
// status is an error.
func TestAnAdoptionWriteTheAPIRefusedIsNotTaken(t *testing.T) {
	cases := []struct {
		name      string
		issues    string
		uncovered []string
		covered   []string
	}{
		{"reopen", closedCompleted, []string{"acme/u"}, nil},
		{"close", openForCovered, nil, []string{"acme/c"}},
	}
	for _, c := range cases {
		for _, status := range []int{403, 500} {
			actions, err := roster.ConvergeAdoption(table(c.issues, status), "acme/manager", c.uncovered, c.covered, nil)
			if err == nil {
				t.Errorf("%s answered %d: took %v and reported no error", c.name, status, actions)
				continue
			}
			if len(actions) != 0 {
				t.Errorf("%s answered %d: reported %v as taken", c.name, status, actions)
			}
			if got := fleet.IsGrant(err); got != (status == 403) {
				t.Errorf("%s answered %d: grant error %v (%v)", c.name, status, got, err)
			}
			if status == 403 && !strings.Contains(err.Error(), "Issues") {
				t.Errorf("%s answered 403 without the Issues hint: %v", c.name, err)
			}
		}
	}
}

// The statuses GitHub answers a taken write with are taken.
func TestAnAdoptionWriteTheAPITookIsAnAction(t *testing.T) {
	gh := func(method, path string, _ any) (fleet.Response, error) {
		switch method {
		case "GET":
			return fleet.Response{Status: 200, JSON: json.RawMessage(closedCompleted)}, nil
		case "PATCH":
			return fleet.Response{Status: 200, JSON: json.RawMessage(`{}`)}, nil
		}
		return fleet.Response{Status: 201, JSON: json.RawMessage(`{}`)}, nil
	}
	actions, err := roster.ConvergeAdoption(gh, "acme/manager", []string{"acme/u"}, nil, nil)
	if err != nil || len(actions) != 1 || actions[0] != "reopened #4 (acme/u)" {
		t.Fatalf("actions %v, error %v", actions, err)
	}
}
