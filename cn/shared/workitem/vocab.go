// Package workitem is the task queue's one durable object: an issue titled
// "[claudinite-work] <pack>/<task> [qualifier]" (or a person's own issue
// adopted into the queue), whose labels are its state, whose body's first
// line is the task path, and whose few body fields are the only facts it
// carries beyond that. The vocabulary here (labels, markers, fields,
// leases, trailers) is the compatibility surface across engine versions,
// string-identical with the Node engine's task-constants.mjs at
// missingbulb/Claudinite@057841ac; the grammar beside it reads every
// spelling any engine ever wrote and writes only today's.
package workitem

import (
	"fmt"
	"strings"
	"time"
)

// WorkPrefix opens a filed work item's title.
const WorkPrefix = "[claudinite-work]"

// The status labels: an item wears one.
const (
	StatusPrefix            = "task:status:"
	ParkPrefix              = StatusPrefix + "needs-human-"
	StatusBlocked           = StatusPrefix + "blocked"
	StatusReady             = StatusPrefix + "waiting-for-executor"
	StatusRunningExecutor   = StatusPrefix + "running-executor"
	StatusRunningAgent      = StatusPrefix + "running-agent"
	StatusNeedsHumanAction  = ParkPrefix + "action"
	StatusNeedsHumanDecide  = ParkPrefix + "decision"
	StatusNeedsHumanApprove = ParkPrefix + "approval"
	StatusNeedsHumanFailure = ParkPrefix + "failure"
	StatusDone              = StatusPrefix + "done"
	StatusRejected          = StatusPrefix + "rejected"
)

// LiveStatuses are the statuses an open item wears before it parks or
// converges.
var LiveStatuses = []string{StatusBlocked, StatusReady, StatusRunningExecutor, StatusRunningAgent}

// ParkKinds are the four parks, in the order a decoder prefers them when
// an item wears more than one: failure first, the conservative lane.
var ParkKinds = []string{"failure", "action", "decision", "approval"}

// ParkStatuses are the park labels, in ParkKinds' order.
var ParkStatuses = func() []string {
	out := make([]string, len(ParkKinds))
	for i, k := range ParkKinds {
		out[i] = ParkPrefix + k
	}
	return out
}()

// StatusLabels is every status label, live first, then the parks, then
// the two terminals.
var StatusLabels = append(append(append([]string{}, LiveStatuses...), ParkStatuses...), StatusDone, StatusRejected)

// The origin labels: who asked for an item, worn for its whole life.
const (
	OriginPrefix  = "task:origin:"
	OriginPlanned = OriginPrefix + "planned"
	OriginManual  = OriginPrefix + "manual"
	OriginAdHoc   = OriginPrefix + "ad-hoc"
)

// OriginLabels are the three origins.
var OriginLabels = []string{OriginPlanned, OriginManual, OriginAdHoc}

// AskedForOrigins are the origins a person's action produces.
var AskedForOrigins = []string{OriginManual, OriginAdHoc}

// Urgent puts an item ahead of every non-urgent one.
const Urgent = "task:urgent"

// The legacy spellings, written never and read forever: labels are stored
// data on open and closed issues in every member.
const (
	LegacyBlocked      = "task:blocked"
	LegacyReady        = "task:ready"
	LegacyExecuting    = "task:executing"
	LegacyAgent        = "task:agent"
	NeedsHuman         = "needs-human"
	OutcomeDelivered   = "outcome:delivered"
	OutcomeDone        = "outcome:done"
	OutcomeObsolete    = "outcome:obsolete"
	LegacyTaskDone     = "task:done"
	LegacyTaskObsolete = "task:obsolete"
	OriginSchedule     = "origin:schedule"
)

// RequeueHint is the re-queue lever in words, written into every message
// that parks an item.
const RequeueHint = "clear its status label and add `" + StatusReady + "`"

// The request vocabulary: claude-task is still accepted as a mark;
// claude-queued and claude-in-review are a legacy shadow item's.
const (
	RequestLabel  = "claude-task"
	QueuedLabel   = "claude-queued"
	InReviewLabel = "claude-in-review"
)

// IsQueueLabel reports a label in the queue's vocabulary, in any spelling
// an engine ever wrote: a person's issue released from the queue keeps
// every other label it carries.
func IsQueueLabel(l string) bool {
	return strings.HasPrefix(l, "task:") || l == NeedsHuman || l == RequestLabel || l == QueuedLabel || l == InReviewLabel
}

// RequestModels are the families a request may ask for.
var RequestModels = []string{"opus", "sonnet", "haiku"}

// Label is one label the queue ensures before applying it.
type Label struct {
	Name, Color, Description string
}

// QueueLabels is every label the engine applies, and the owner's closed list
// of the labels it may: the legacy spellings above are read, never written.
var QueueLabels = []Label{
	{StatusBlocked, "c5def5", "Claudinite queue: waiting on Blocked-by and/or Not-before"},
	{StatusReady, "0e8a16", "Claudinite queue: available for an executor to pick up"},
	{Urgent, "d93f0b", "Claudinite queue: pick this before any non-urgent item"},
	{StatusRunningExecutor, "fbca04", "Claudinite queue: an executor holds the claim"},
	{StatusRunningAgent, "1d76db", "Claudinite queue: an agent session owns this item"},
	{StatusNeedsHumanAction, "b60205", "Claudinite queue: parked — a human must change something outside the code"},
	{StatusNeedsHumanDecide, "d93f0b", "Claudinite queue: parked — a human must choose what happens next"},
	{StatusNeedsHumanApprove, "5319e7", "Claudinite queue: parked — succeeded and left an unmerged PR to approve"},
	{StatusNeedsHumanFailure, "b60205", "Claudinite queue: parked — the run broke, diagnose and fix"},
	{StatusDone, "0e8a16", "Claudinite queue: succeeded, nothing pending"},
	{StatusRejected, "ededed", "Claudinite queue: never ran — the precondition said no, or the task is gone"},
	{OriginPlanned, "c2e0c6", "Claudinite queue: filed by the schedule — a task's own occurrence"},
	{OriginManual, "bfd4f2", "Claudinite queue: pulled by a person — an occurrence of a declared task, woken or hand-created"},
	{OriginAdHoc, "bfd4f2", "Claudinite queue: asked for by a person — their own issue, adopted as the work item itself"},
}

// The comment markers the protocol reads back.
const (
	ClaimMarker     = "<!-- claudinite-claim -->"
	HandoffMarker   = "<!-- claudinite-handoff -->"
	EpisodeMarker   = "<!-- claudinite-episode -->"
	HeartbeatMarker = "<!-- claudinite-heartbeat -->"
)

// The body fields.
const (
	NotBeforeField    = "Not-before"
	BlockedByField    = "Blocked-by"
	EndsWhenField     = "Ends-when"
	EndsWhenClosed    = "closed"
	TargetBranchField = "Target-branch"
	TargetPRField     = "Target-pr"
	SupersedesField   = "Supersedes"
	WokenField        = "Woken"
	RequestField      = "Request"
	ModelField        = "Model"
	TaskField         = "Task"
	MergeField        = "Merge"
	MergeIfNarrow     = "if-narrow"
	DeliveredHeading  = "Delivered by code-work"
	ProgressHeading   = "Progress"
	LastVerdictHead   = "Last verdict"
	MachineBlockStart = "<!-- claudinite-item -->"
	MachineBlockEnd   = "<!-- /claudinite-item -->"
)

// LegacyDeliveredHeadings are earlier spellings of DeliveredHeading.
var LegacyDeliveredHeadings = []string{"Delivered by prework", "Delivered by code_work"}

// The leases and bounds three surfaces agree on.
const (
	ExecutingLeash    = 60 * time.Minute
	AgentLeash        = 3 * time.Hour
	StaleReadyPeriods = 2
	StuckBlocked      = 48 * time.Hour
	TerminalOpen      = time.Hour
	AbandonedPark     = 10 * 24 * time.Hour
	Heartbeat         = 15 * time.Minute
)

// The commit trailers.
const (
	TaskTrailer      = "Claudinite-Task"
	AutomergeTrailer = "Claudinite-Automerge-Policy"
)

// The declaration defaults.
const (
	DefaultAutomerge  = "nothing"
	DefaultAgentModel = "none"
)

// RoutineTokenSecret is the Actions secret holding the executor
// routine's token where the member's routines name none.
const RoutineTokenSecret = "CCR_ROUTINE_TOKEN"

// DormantConfigKey is the tasks block's key that stops a repo's
// recurring work.
const DormantConfigKey = "dormant"

// InstructionsFile is where SessionStart writes RoutineInstructions in
// every member; the file ignores itself, so it is always the pinned
// engine's copy.
const InstructionsFile = ".claudinite/cache/instructions.md"

// RoutinePrompt is the executor routine's whole stored prompt: the
// procedure itself ships in the engine the member pins.
const RoutinePrompt = "Read `" + InstructionsFile + "` and follow it."

// The two member workflow files.
const (
	SchedulerWorkflowFile = "claudinite-scheduler.yml"
	ExecutorWorkflowFile  = "claudinite-executor.yml"
)

// Approved reports whether the engine may write label.
func Approved(label string) bool {
	for _, l := range QueueLabels {
		if l.Name == label {
			return true
		}
	}
	return false
}

// RefuseUnapproved names the first label not on the approved list, or is nil.
func RefuseUnapproved(labels ...string) error {
	for _, l := range labels {
		if !Approved(l) {
			return fmt.Errorf("label %q is not on the approved list", l)
		}
	}
	return nil
}
