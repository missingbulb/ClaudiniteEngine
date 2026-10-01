package main

import (
	"strings"
	"testing"
)

func TestUpdateCommandArguments(t *testing.T) {
	bin := buildCN(t, "")
	noToken := []string{"GITHUB_TOKEN="}
	for _, c := range []struct {
		args []string
		code int
		in   string
	}{
		{[]string{"update"}, 2, "update takes engine or land"},
		{[]string{"update", "bogus"}, 2, "update takes engine or land"},
		{[]string{"update", "land"}, 2, "--pr"},
		{[]string{"update", "land", "--pr", "3"}, 2, "--sha"},
		{[]string{"update", "engine", "--bogus"}, 2, ""},
		{[]string{"update", "engine", "--repo", t.TempDir()}, 1, "GITHUB_TOKEN"},
		{[]string{"update", "land", "--pr", "3", "--sha", "abc", "--repo", t.TempDir()}, 1, "GITHUB_TOKEN"},
	} {
		_, errOut, code := runCN(t, bin, noToken, "", c.args...)
		if code != c.code || !strings.Contains(errOut, c.in) {
			t.Errorf("%v: exit %d %q", c.args, code, errOut)
		}
	}
	if _, errOut, _ := runCN(t, bin, nil, "", "bogus"); !strings.Contains(errOut, "update engine [--force]") {
		t.Errorf("usage lacks update: %s", errOut)
	}
}

func TestWorkflowsDiffCommand(t *testing.T) {
	bin := buildCN(t, "")
	out, _, code := runCN(t, bin, nil, "", "workflows", "diff", "--repo", t.TempDir())
	if code != 0 || !strings.Contains(out, "+++ b/.github/workflows/claudinite-update.yml") {
		t.Errorf("exit %d\n%s", code, out)
	}
	if _, _, code := runCN(t, bin, nil, "", "workflows"); code != 2 {
		t.Errorf("no subcommand: exit %d", code)
	}
}
