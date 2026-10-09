package roster

import (
	"strings"
	"testing"
)

// A repo outside the fleet is named in the report with how to bring it
// in or leave it out; the sweep files nothing about it.
func TestTheCoverageReportNamesTheReposOutsideTheFleet(t *testing.T) {
	out := RenderCoverage("acme", "acme/manager", Coverage{Covered: []string{"acme/a"}, Uncovered: []string{"acme/b"}})
	if !strings.Contains(out, "**Outside the fleet:** acme/b. To bring one in, run `cn init`") || !strings.Contains(out, "`exclude`") {
		t.Errorf("%s", out)
	}
	if strings.Contains(out, "Issue actions") || strings.Contains(out, "issue") {
		t.Errorf("the report speaks of issues:\n%s", out)
	}
	if out := RenderCoverage("acme", "acme/manager", Coverage{Covered: []string{"acme/a"}}); !strings.Contains(out, "**Outside the fleet:** none") {
		t.Errorf("%s", out)
	}
}
