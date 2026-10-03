package update_test

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/update"
)

// A member whose declaration the token may not read is the grant's
// problem, as a refused dispatch is: the run's failure carries the
// action marker either way.
func TestADeclarationTheGrantRefusesKeepsTheActionLane(t *testing.T) {
	gh := func(string, string, any) (fleet.Response, error) { return fleet.Response{Status: 403}, nil }
	r := fleet.Repo{Name: "m", FullName: "acme/m"}
	r.Owner.Login = "acme"
	_, _, failed := update.Force(gh, []fleet.Repo{r}, fleet.Config{Owner: "acme"}, nil, update.Options{Now: func() string { return "" }})
	rep := update.Report{Owner: "acme", Failed: failed}
	if len(failed) != 1 || failed[0].State != "error" || !rep.Grant() {
		t.Fatalf("failed %+v, grant %v", failed, rep.Grant())
	}
}

func TestAnUnreadableDeclarationIsNoGrantError(t *testing.T) {
	gh := func(string, string, any) (fleet.Response, error) { return fleet.Response{Status: 500}, nil }
	r := fleet.Repo{Name: "m", FullName: "acme/m"}
	_, _, failed := update.Force(gh, []fleet.Repo{r}, fleet.Config{Owner: "acme"}, nil, update.Options{Now: func() string { return "" }})
	if rep := (update.Report{Failed: failed}); len(failed) != 1 || rep.Grant() {
		t.Fatalf("failed %+v", failed)
	}
}
