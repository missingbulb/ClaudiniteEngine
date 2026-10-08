package mergepolicy

import (
	"strings"
	"testing"
)

const (
	pinBefore = "engine:\n  package: \"@claudinite/cli\"\n  version: \"1.61001.1\"\n  manifest: \"sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==\"\npacks:\n  declared:\n    - hello\n"
	pinAfter  = "engine:\n  package: \"@claudinite/cli\"\n  version: \"1.61001.2\"\n  manifest: \"sha512-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB==\"\npacks:\n  declared:\n    - hello\n"
)

// updatePolicy is the engine/update task's prediction of its pull
// request: the pin moved, and the member file, the launcher and the
// managed workflows.
var updatePolicy = []any{"engine-pin-move", "engine-update-files"}

func modified(file, before, after string) Entry {
	return Entry{File: file, Before: &before, After: &after}
}
func added(file, after string) Entry    { return Entry{File: file, After: &after} }
func deleted(file, before string) Entry { return Entry{File: file, Before: &before} }

// A real engine update PR: the pin moved, the member file restated, a
// managed workflow moved in or added.
func TestTheEngineUpdatePolicyPassesARealUpdatePR(t *testing.T) {
	shapes := map[string][]Entry{
		"the pin alone": {modified(".claudinite/settings.yaml", pinBefore, pinAfter)},
		"the pin, the member file and moved workflows": {
			modified(".claudinite/settings.yaml", pinBefore, pinAfter),
			modified(".claudinite/cache/member.GENERATED.json", "{\"version\": \"1\"}\n", "{\"version\": \"2\"}\n"),
			modified(".github/workflows/claudinite-scheduler.yml", "on: {}\n", "on:\n  schedule: []\n"),
			added(".github/workflows/claudinite-executor.yml", "on: {}\n"),
			modified(".github/workflows/claudinite-ci.yml", "a\n", "b\n"),
		},
		"the pin and the launcher the new engine ships": {
			modified(".claudinite/settings.yaml", pinBefore, pinAfter),
			modified(".claudinite/launch", "#!/bin/sh\n# 1\n", "#!/bin/sh\n# 2\n"),
		},
		"a member file still in the legacy directory": {
			modified(".claudinite/settings.yaml", pinBefore, pinAfter),
			modified(".claudinite/flat/member.GENERATED.json", "{}\n", "{\"v\": 2}\n"),
		},
	}
	for name, entries := range shapes {
		if v := Judge(updatePolicy, entries, Declared{}); !v.Mergeable {
			t.Errorf("%s: %s", name, v.Why)
		}
	}
}

// Anything wider parks: another settings edit, a workflow the engine does
// not manage, a deleted one, a staged file left behind, any extra file;
// and the pin exception is the class's own, never a conjunction's.
func TestTheEngineUpdatePolicyRefusesAnythingElse(t *testing.T) {
	pin := modified(".claudinite/settings.yaml", pinBefore, pinAfter)
	cases := map[string]struct {
		policy  any
		entries []Entry
		want    string
	}{
		"a pack declared beside the pin": {updatePolicy, []Entry{modified(".claudinite/settings.yaml", pinBefore, strings.Replace(pinAfter, "- hello", "- hello\n    - other", 1))}, "settings.yaml"},
		"the engine package changed":     {updatePolicy, []Entry{modified(".claudinite/settings.yaml", pinBefore, strings.Replace(pinAfter, "cli\"", "cli-rc\"", 1))}, "settings.yaml"},
		"a settings file written whole":  {updatePolicy, []Entry{added(".claudinite/settings.yaml", pinAfter)}, "settings.yaml"},
		"a workflow of the repo's own":   {updatePolicy, []Entry{pin, modified(".github/workflows/deploy.yml", "a\n", "b\n")}, "deploy.yml"},
		"a deleted managed workflow":     {updatePolicy, []Entry{pin, deleted(".github/workflows/claudinite-ci.yml", "a\n")}, "claudinite-ci.yml"},
		"a staged workflow left behind":  {updatePolicy, []Entry{pin, added(".claudinite/cache/pending-workflows/claudinite-ci.yml", "a\n")}, "pending-workflows"},
		"an extra file":                  {updatePolicy, []Entry{pin, modified("README.md", "a\n", "b\n")}, "README.md"},
		"a deleted launcher":             {updatePolicy, []Entry{pin, deleted(".claudinite/launch", "#!/bin/sh\n")}, "launch"},
		"another task's declaration":     {updatePolicy, []Entry{pin, modified(".claudinite/local/packs/acme-pack/tasks/acme-task/task.json", "{}\n", "{\"x\": 1}\n")}, "task.json"},
		"the pin through a conjunction":  {[]any{"engine-pin-move&&under:.claudinite"}, []Entry{pin}, "settings.yaml"},
		"the pin under a folder scope":   {[]any{"under:.claudinite"}, []Entry{pin}, "settings.yaml"},
	}
	for name, c := range cases {
		v := Judge(c.policy, c.entries, Declared{})
		if v.Mergeable || !strings.Contains(v.Why, c.want) {
			t.Errorf("%s: mergeable %v: %s", name, v.Mergeable, v.Why)
		}
	}
}
