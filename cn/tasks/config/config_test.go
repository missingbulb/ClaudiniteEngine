package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoWith(t *testing.T, settings string) string {
	t.Helper()
	repo := t.TempDir()
	p := filepath.Join(repo, ".claudinite", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("engine:\n  version: \"1.1.0\"\n"+settings), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// A repo with no tasks block runs the queue on every default: no
// routines, auto-merge delivery, nothing disabled, awake.
func TestNoTasksBlockReadsAsTheDefaults(t *testing.T) {
	t.Parallel()
	c, err := Read(repoWith(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Routines) != 0 || c.Delivery != AutoMerge || len(c.Disabled) != 0 || c.Dormant || c.MinuteRate != nil {
		t.Errorf("%+v", c)
	}
}

func TestTheTasksBlockReadsEachKey(t *testing.T) {
	t.Parallel()
	c, err := Read(repoWith(t, `tasks:
  routines:
    default:
      url: https://example.test/fire
  delivery: review
  disabled:
    - acme-pack/acme-task
  dormant: true
  actionsMinuteRate: 0.008
`))
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := c.Routines["default"].(map[string]any); e["url"] != "https://example.test/fire" {
		t.Errorf("routines %v", c.Routines)
	}
	if c.Delivery != Review || len(c.Disabled) != 1 || c.Disabled[0] != "acme-pack/acme-task" || !c.Dormant || c.MinuteRate == nil || *c.MinuteRate != 0.008 {
		t.Errorf("%+v", c)
	}
}

// Every malformed key fails the read with the key named, so a run never
// proceeds on a value it misread.
func TestAMalformedTasksBlockIsRefused(t *testing.T) {
	t.Parallel()
	for block, want := range map[string]string{
		"tasks:\n  delivery: sometimes\n":                "delivery",
		"tasks:\n  dormant: maybe\n":                     "dormant",
		"tasks:\n  disabled: acme-pack/acme-task\n":      "disabled",
		"tasks:\n  disabled: [3]\n":                      "disabled",
		"tasks:\n  routines: [a]\n":                      "routines",
		"tasks:\n  actionsMinuteRate: cheap\n":           "actionsMinuteRate",
		"tasks:\n  actionsMinuteRate: -1\n":              "actionsMinuteRate",
		"tasks:\n  agenticTaskInvocationEndpoints: {}\n": "agenticTaskInvocationEndpoints",
	} {
		if _, err := Read(repoWith(t, block)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", block, err)
		}
	}
}

// A member still declaring the retired claudinite-tasks entry keeps its
// config until it moves to the tasks block, which wins once both exist.
func TestTheRetiredPackEntrysConfigReadsAsTheTasksBlock(t *testing.T) {
	t.Parallel()
	entry := `packs:
  declared:
    - id: claudinite-tasks
      config:
        agenticTaskInvocationEndpoints:
          default:
            url: https://example.test/fire
        dailyClaudiniteUpdatesRequirePrReview: true
        disabledTasks: [acme-pack/acme-task]
        dormant: true
        actionsMinuteRate: 0.008
`
	c, err := Read(repoWith(t, entry))
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := c.Routines["default"].(map[string]any); e["url"] != "https://example.test/fire" || c.Delivery != Review || len(c.Disabled) != 1 || !c.Dormant || c.MinuteRate == nil || !c.Legacy {
		t.Errorf("%+v", c)
	}
	both, err := Read(repoWith(t, entry+"tasks:\n  delivery: auto-merge\n"))
	if err != nil || both.Delivery != AutoMerge || both.Dormant || !both.Legacy {
		t.Errorf("%+v %v", both, err)
	}
	if plain, _ := Read(repoWith(t, "tasks:\n  dormant: true\n")); plain.Legacy {
		t.Error("a tasks block alone reads as legacy")
	}
}
