package queue

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The machine-readable records a run leaves in its Actions log, rendered
// and parsed in one place so a format cannot drift from itself.

// The execution record: what an executor did with one item.
const (
	TaskExecTag = "claudinite-task-exec"
	TaskRunTag  = "claudinite-task-run"
	RunCostTag  = "claudinite-run-cost"
	recordV     = "v1"
)

// TaskExecStatuses are an execution record's statuses.
var TaskExecStatuses = []string{"success", "failed", "task-gone", "invalid"}

// TaskRunOutcomes are the retired slot scheduler's outcomes, read from
// logs still inside their retention window and never written.
var TaskRunOutcomes = []string{"agent", "code-work", "skipped", "failed", "deferred"}

// TaskExec is one execution record; Slot is the occurrence's identity
// (the item's #number under the queue).
type TaskExec struct {
	Pack   string `json:"pack"`
	Task   string `json:"task"`
	Slot   string `json:"slotId"`
	Status string `json:"status"`
}

// RenderTaskExec is the record's line.
func RenderTaskExec(r TaskExec) string {
	slot := r.Slot
	if slot == "" {
		slot = "unknown"
	}
	return fmt.Sprintf("%s %s %s/%s [%s] %s", TaskExecTag, recordV, r.Pack, r.Task, slot, r.Status)
}

var (
	execLineRE = regexp.MustCompile(`(?:^|\s)` + TaskExecTag + ` ` + recordV + ` (\S+)/(\S+) \[(\S+)\] ([a-z-]+)\s*$`)
	runLineRE  = regexp.MustCompile(`^(?:\S+\s+)?` + TaskRunTag + ` ` + recordV + ` (\S+)/(\S+) \[(\S+)\] ([a-z-]+)\s*$`)
	costLineRE = regexp.MustCompile(`(?:^|\s)` + RunCostTag + ` ` + recordV + ` ([a-z-]+) \[(\S+)\]((?: [a-z-]+=\d+)*)\s*$`)
)

func parseRecord(re *regexp.Regexp, legal []string, line string) (TaskExec, bool) {
	m := re.FindStringSubmatch(line)
	if m == nil || !inList(legal, m[4]) {
		return TaskExec{}, false
	}
	return TaskExec{Pack: m[1], Task: m[2], Slot: m[3], Status: m[4]}, true
}

// ParseTaskExec reads one execution record; an unknown status is not one.
func ParseTaskExec(line string) (TaskExec, bool) {
	return parseRecord(execLineRE, TaskExecStatuses, line)
}

// ParseTaskRun reads one of the retired slot scheduler's records; Status
// carries its outcome.
func ParseTaskRun(line string) (TaskExec, bool) {
	return parseRecord(runLineRE, TaskRunOutcomes, line)
}

// ParseTaskExecs is every execution record in a log.
func ParseTaskExecs(text string) []TaskExec {
	out := []TaskExec{}
	for _, l := range strings.Split(text, "\n") {
		if r, ok := ParseTaskExec(l); ok {
			out = append(out, r)
		}
	}
	return out
}

// RunWorkflows are the two workflows a cost record describes, and
// RunPhases the phases each times, in order.
var (
	RunWorkflows = []string{"scheduler", "executor"}
	RunPhases    = map[string][]string{
		"scheduler": {"list", "ask", "repair", "drain"},
		"executor":  {"pick", "claim", "code-work", "hand-off", "converge"},
	}
	allPhases = []string{"list", "ask", "repair", "drain", "pick", "claim", "code-work", "hand-off", "converge"}
)

// RunCost is what one run of the machinery spent: the API calls it made
// (nil when unread) and the milliseconds each phase took. A phase that did
// not happen has no key: unknown is not zero.
type RunCost struct {
	Workflow string             `json:"workflow"`
	RunID    string             `json:"runId"`
	APICalls *int               `json:"apiCalls"`
	PhaseMs  map[string]float64 `json:"phaseMs"`
}

// RenderRunCost is the cost record's line.
func RenderRunCost(c RunCost) string {
	id := c.RunID
	if id == "" {
		id = "unknown"
	}
	parts := []string{fmt.Sprintf("%s %s %s [%s]", RunCostTag, recordV, c.Workflow, id)}
	if c.APICalls != nil {
		parts = append(parts, "calls="+strconv.Itoa(*c.APICalls))
	}
	for _, p := range allPhases {
		if ms, ok := c.PhaseMs[p]; ok {
			parts = append(parts, p+"="+strconv.FormatInt(int64(math.Floor(ms+0.5)), 10))
		}
	}
	return strings.Join(parts, " ")
}

// ParseRunCost reads one cost record: an unknown workflow rejects the
// line, an unknown phase is dropped and the rest stands.
func ParseRunCost(line string) (RunCost, bool) {
	m := costLineRE.FindStringSubmatch(line)
	if m == nil || !inList(RunWorkflows, m[1]) {
		return RunCost{}, false
	}
	c := RunCost{Workflow: m[1], RunID: m[2], PhaseMs: map[string]float64{}}
	for _, f := range strings.Fields(m[3]) {
		name, value, _ := strings.Cut(f, "=")
		n, _ := strconv.ParseFloat(value, 64)
		if name == "calls" {
			v := int(n)
			c.APICalls = &v
		} else if inList(allPhases, name) {
			c.PhaseMs[name] = n
		}
	}
	return c, true
}

// CostMeter times a run's phases; reopening a phase adds to it, since the
// executor passes through pick, claim and converge once per item.
type CostMeter struct {
	Workflow, RunID string
	Calls           func() *int
	Now             func() time.Time
	phaseMs         map[string]float64
}

// Phase opens a phase and returns the function that closes it.
func (m *CostMeter) Phase(name string) func() {
	if m.phaseMs == nil {
		m.phaseMs = map[string]float64{}
	}
	from := m.Now()
	return func() { m.phaseMs[name] += float64(m.Now().Sub(from).Milliseconds()) }
}

// Record is the record as it stands now: a snapshot.
func (m *CostMeter) Record() string {
	var calls *int
	if m.Calls != nil {
		calls = m.Calls()
	}
	return RenderRunCost(RunCost{Workflow: m.Workflow, RunID: m.RunID, APICalls: calls, PhaseMs: m.phaseMs})
}

func inList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
