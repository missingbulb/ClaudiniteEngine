package mergepolicy

import (
	"strings"
	"testing"
)

// usageFoldPolicy is the engine/usage-fold task's prediction of its pull
// request: the two rolling files written, and their legacy copies moved.
var usageFoldPolicy = []any{"rolling-usage-files", "rolling-usage-file-moves"}

func TestTheUsageFoldPolicyPassesARealFoldPR(t *testing.T) {
	shapes := map[string][]Entry{
		"both files refolded": {
			modified(".claudinite/usage/sessions-and-elements.json", "{}\n", "{\"v\": 1}\n"),
			modified(".claudinite/usage/task-runs-and-costs.json", "{}\n", "{\"v\": 1}\n"),
		},
		"a first fold": {added(".claudinite/usage/task-runs-and-costs.json", "{}\n")},
		"both files moved home": {
			deleted(".claudinite/local/usage.GENERATED.json", "{}\n"),
			added(".claudinite/usage/sessions-and-elements.json", "{}\n"),
			deleted(".claudinite/local/tasks-usage.GENERATED.json", "{}\n"),
			added(".claudinite/usage/task-runs-and-costs.json", "{}\n"),
		},
	}
	for name, entries := range shapes {
		if v := Judge(usageFoldPolicy, entries, Declared{}); !v.Mergeable {
			t.Errorf("%s: %s", name, v.Why)
		}
	}
}

func TestTheUsageFoldPolicyRefusesAnythingElse(t *testing.T) {
	cases := map[string]struct {
		entries []Entry
		want    string
	}{
		"a rolling file deleted":         {[]Entry{deleted(".claudinite/usage/task-runs-and-costs.json", "{}\n")}, "task-runs-and-costs.json"},
		"a file nested under usage":      {[]Entry{added(".claudinite/usage/old/a.json", "{}\n")}, "old/a.json"},
		"a file of another kind":         {[]Entry{added(".claudinite/usage/notes.md", "x\n")}, "notes.md"},
		"a legacy copy edited in place":  {[]Entry{modified(".claudinite/local/usage.GENERATED.json", "{}\n", "{\"v\": 1}\n")}, "usage.GENERATED.json"},
		"a legacy copy written anew":     {[]Entry{added(".claudinite/local/tasks-usage.GENERATED.json", "{}\n")}, "tasks-usage.GENERATED.json"},
		"another generated file deleted": {[]Entry{deleted(".claudinite/local/other.GENERATED.json", "{}\n")}, "other.GENERATED.json"},
		"an extra file":                  {[]Entry{added(".claudinite/usage/a.json", "{}\n"), modified("README.md", "a\n", "b\n")}, "README.md"},
	}
	for name, c := range cases {
		v := Judge(usageFoldPolicy, c.entries, Declared{})
		if v.Mergeable || !strings.Contains(v.Why, c.want) {
			t.Errorf("%s: mergeable %v: %s", name, v.Mergeable, v.Why)
		}
	}
}

func TestTheUsageFoldClassesAreBuiltIn(t *testing.T) {
	names := strings.Join(BuiltinNames(), " ")
	for _, n := range []string{"rolling-usage-files", "rolling-usage-file-moves"} {
		if _, ok := Builtins[n]; !ok || !strings.Contains(names, n) {
			t.Errorf("%s is not a built-in class", n)
		}
	}
}
