package roster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/roster"
)

func verdicts() []fleet.Verdict {
	yes := true
	fresh := fleet.Classify(fleet.FreshIn{Shape: fleet.ShapeNode})
	return []fleet.Verdict{
		{Repo: "acme/Zeta", DefaultBranch: "main", Scope: fleet.ScopeIn, Shape: fleet.ShapeNode, Settings: ".claudinite-settings.json", Covered: true, Freshness: &fresh},
		{Repo: "acme/manager", DefaultBranch: "main", Scope: fleet.ScopeHome},
		{Repo: "acme/alpha", DefaultBranch: "trunk", Scope: fleet.ScopeIn, Shape: fleet.ShapeCn, Settings: ".claudinite/settings.yaml", Covered: true,
			Pin: "61003.1.0", Held: fleet.Held{"basics": "61003.1"}, HasScheduler: &yes, Error: "GET /repos/acme/alpha/contents/x returned 500"},
	}
}

func TestRenderPinsTheRosterBytes(t *testing.T) {
	got, err := roster.Render("acme", verdicts(), "2026-10-03T06:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 1,
  "generated": "2026-10-03T06:00:00Z",
  "owner": "acme",
  "members": [
    {
      "repo": "acme/alpha",
      "defaultBranch": "trunk",
      "scope": "in",
      "shape": "cn",
      "settings": ".claudinite/settings.yaml",
      "covered": true,
      "dormant": false,
      "pin": "61003.1.0",
      "held": {
        "basics": "61003.1"
      },
      "hasScheduler": true,
      "error": "GET /repos/acme/alpha/contents/x returned 500"
    },
    {
      "repo": "acme/manager",
      "defaultBranch": "main",
      "scope": "home",
      "covered": false,
      "dormant": false
    },
    {
      "repo": "acme/Zeta",
      "defaultBranch": "main",
      "scope": "in",
      "shape": "node",
      "settings": ".claudinite-settings.json",
      "covered": true,
      "dormant": false,
      "freshness": {
        "state": "node",
        "detail": "runs the Node engine; moves with phase 9"
      }
    }
  ]
}
`
	if got != want {
		t.Errorf("roster file:\n%s\nwant:\n%s", got, want)
	}
}

// A recompute that moves only the generated stamp writes nothing; a
// changed verdict writes the new stamp with it.
func TestWriteArtifactIgnoresTheStamp(t *testing.T) {
	root := t.TempDir()
	if wrote, err := roster.WriteArtifact(root, "acme", verdicts(), "2026-10-03T06:00:00Z", nil); err != nil || !wrote {
		t.Fatalf("first write %v %v", wrote, err)
	}
	if wrote, err := roster.WriteArtifact(root, "acme", verdicts(), "2026-10-04T06:00:00Z", nil); err != nil || wrote {
		t.Fatalf("a stamp-only recompute wrote %v %v", wrote, err)
	}
	moved := verdicts()
	moved[0].Dormant = true
	if wrote, err := roster.WriteArtifact(root, "acme", moved, "2026-10-05T06:00:00Z", nil); err != nil || !wrote {
		t.Fatalf("a changed verdict wrote %v %v", wrote, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(roster.RosterFile)))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := roster.Render("acme", moved, "2026-10-05T06:00:00Z")
	if string(raw) != want {
		t.Errorf("roster file after the change:\n%s", raw)
	}
}

// Every enumerated repository has a row, the manager's own with scope
// home, read from the same walk as the report.
func TestBuildJudgesEveryRepository(t *testing.T) {
	none := func(method, path string, _ any) (fleet.Response, error) {
		t.Errorf("read %s %s of a repository out of scope", method, path)
		return fleet.Response{Status: 500}, nil
	}
	repos := []fleet.Repo{{FullName: "acme/manager"}, {FullName: "acme/old", Archived: true}, {FullName: "acme/fork", Fork: true}}
	got := roster.Verdicts(roster.Build(none, repos, "acme/manager", fleet.Config{Owner: "acme"}, nil))
	if len(got) != 3 || got[0].Scope != fleet.ScopeHome || got[1].Repo != "acme/old" || got[1].Scope == fleet.ScopeIn || got[2].Scope == fleet.ScopeIn {
		t.Errorf("verdicts %+v", got)
	}
}

// The branch's copy is the prior when one is handed in: a checkout
// lacking the file still writes nothing new over a branch whose roster
// differs only in its stamp, and is left holding the branch's bytes.
func TestWriteArtifactReadsTheBranchsCopy(t *testing.T) {
	landed, _ := roster.Render("acme", verdicts(), "2026-10-01T06:00:00Z")
	root := t.TempDir()
	if wrote, err := roster.WriteArtifact(root, "acme", verdicts(), "2026-10-03T06:00:00Z", []byte(landed)); err != nil || wrote {
		t.Fatalf("a stamp-only recompute over the branch's copy wrote %v %v", wrote, err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(roster.RosterFile)))
	if string(raw) != landed {
		t.Errorf("the checkout holds %q, not the branch's bytes", raw)
	}
	moved := verdicts()
	moved[1].Covered = true
	if wrote, err := roster.WriteArtifact(root, "acme", moved, "2026-10-03T06:00:00Z", []byte(landed)); err != nil || !wrote {
		t.Fatalf("a changed verdict over the branch's copy wrote %v %v", wrote, err)
	}
}
