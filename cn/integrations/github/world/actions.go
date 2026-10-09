package world

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Env is the Actions job's environment as the runner reads it, through a
// getenv a test replaces.
type Env func(string) string

// SuspendAllVar is the operator hold: set, the queue is held, and every
// workflow exits before its first read.
const SuspendAllVar = "CLAUDINITE_TASKS_SUSPEND_ALL"

// VarsBagEnv carries every repository variable as JSON (toJSON(vars)),
// so a variable the workflow file never names still reaches the run.
const VarsBagEnv = "CLAUDINITE_VARS"

// NowEnv replaces the clock with a fixed instant, for the rehearsal.
const NowEnv = "CLAUDINITE_NOW"

// Vars is the repository-variable bag; nil when absent or unreadable.
func (e Env) Vars() map[string]any {
	raw := e(VarsBagEnv)
	if raw == "" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// Var is a repository variable: the bag's, else the environment's.
func (e Env) Var(name string) string {
	if v, ok := e.Vars()[name]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return e(name)
}

// Suspended reports the operator hold.
func (e Env) Suspended() bool {
	switch strings.ToLower(strings.TrimSpace(e.Var(SuspendAllVar))) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// SuspendedNotice is what a held workflow prints before it exits.
func SuspendedNotice() string {
	return "- " + SuspendAllVar + " is set: the queue is held. Nothing is picked up, created, readied or reclaimed.\n" +
		"  Clear the variable in repo settings (Settings → Secrets and variables → Actions → Variables) to resume;" +
		" the next scheduled scheduler run recovers everything on its own."
}

// Repo is the repository the job runs for.
func (e Env) Repo() string { return e("GITHUB_REPOSITORY") }

// DefaultBranch is the branch the scheduled and dispatched runs check out.
func (e Env) DefaultBranch() string {
	if b := e("GITHUB_REF_NAME"); b != "" {
		return b
	}
	return "main"
}

// RunURL is this run's page, "" outside Actions.
func (e Env) RunURL() string {
	id := e("GITHUB_RUN_ID")
	if id == "" {
		return ""
	}
	server := e("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}
	return server + "/" + e.Repo() + "/actions/runs/" + id
}

// ExecutorID is the executor's self-declared identity.
func (e Env) ExecutorID() string {
	if id := e("CLAUDINITE_EXECUTOR_ID"); id != "" {
		return id
	}
	if id := e("GITHUB_RUN_ID"); id != "" {
		return "actions-" + id
	}
	return "actions-local"
}

// SetOutput appends a step output; an error when the job maps none.
func (e Env) SetOutput(name, value string) error {
	path := e("GITHUB_OUTPUT")
	if path == "" {
		return fmt.Errorf("GITHUB_OUTPUT is not set")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%s=%s\n", name, value); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Clock is the job's clock: CLAUDINITE_NOW where set, the wall clock
// otherwise.
func (e Env) Clock() (Clock, error) {
	raw := e(NowEnv)
	if raw == "" {
		return RealClock{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, fmt.Errorf("%s=%q is not an instant: %w", NowEnv, raw, err)
	}
	return FixedClock(t.UTC()), nil
}
