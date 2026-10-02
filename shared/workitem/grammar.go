package workitem

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Issue is a work item as the queue reads it, projected from the issues
// API: the fields every stage reads, with labels as names.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Labels    LabelList `json:"labels"`
	CreatedAt string    `json:"created_at,omitempty"`
	ClosedAt  string    `json:"closed_at,omitempty"`
	UpdatedAt string    `json:"updated_at,omitempty"`
}

// LabelList is an issue's label names; it decodes from the API's objects
// or from bare strings.
type LabelList []string

// UnmarshalJSON accepts ["a", {"name": "b"}].
func (l *LabelList) UnmarshalJSON(raw []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	out := LabelList{}
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			if s != "" {
				out = append(out, s)
			}
			continue
		}
		var o struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(it, &o) == nil && o.Name != "" {
			out = append(out, o.Name)
		}
	}
	*l = out
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// HasLabel reports whether the issue wears name.
func (i Issue) HasLabel(name string) bool { return contains(i.Labels, name) }

var legacyStatus = []struct{ from, to string }{
	{LegacyBlocked, StatusBlocked},
	{LegacyReady, StatusReady},
	{LegacyExecuting, StatusRunningExecutor},
	{LegacyAgent, StatusRunningAgent},
	{LegacyTaskDone, StatusDone}, {OutcomeDone, StatusDone},
	{LegacyTaskObsolete, StatusRejected}, {OutcomeObsolete, StatusRejected},
}

func legacyOf(name string) (string, bool) {
	for _, l := range legacyStatus {
		if l.from == name {
			return l.to, true
		}
	}
	return "", false
}

var legacyParkRE = regexp.MustCompile(`^task:needs-human-(.+)$`)

// parkOf is the park the labels name, canonical, or "". A kind nobody
// here knows reads as failure, the conservative lane.
func parkOf(names []string) string {
	var kinds []string
	for _, n := range names {
		if strings.HasPrefix(n, ParkPrefix) {
			kinds = append(kinds, n[len(ParkPrefix):])
		}
	}
	for _, n := range names {
		if m := legacyParkRE.FindStringSubmatch(n); m != nil {
			kinds = append(kinds, m[1])
		}
	}
	if len(kinds) == 0 && !contains(names, NeedsHuman) {
		return ""
	}
	for _, k := range ParkKinds {
		if contains(kinds, k) {
			return ParkPrefix + k
		}
	}
	return ParkPrefix + "failure"
}

// StatusesOn is every distinct status the labels decode to, a park
// first.
func StatusesOn(labels []string) []string {
	var out []string
	add := func(s string) {
		if !contains(out, s) {
			out = append(out, s)
		}
	}
	if p := parkOf(labels); p != "" {
		add(p)
	}
	for _, n := range labels {
		if contains(StatusLabels, n) && !strings.HasPrefix(n, ParkPrefix) {
			add(n)
		} else if c, ok := legacyOf(n); ok {
			add(c)
		}
	}
	return out
}

// StatusOf is the status the labels wear, canonical, or "" for none. A
// park wins over anything else present.
func StatusOf(labels []string) string {
	worn := StatusesOn(labels)
	for _, s := range worn {
		if strings.HasPrefix(s, ParkPrefix) {
			return s
		}
	}
	for _, s := range StatusLabels {
		if contains(worn, s) {
			return s
		}
	}
	return ""
}

// Status is the issue's status, canonical.
func (i Issue) Status() string { return StatusOf(i.Labels) }

// Is reports whether the issue's status is status.
func (i Issue) Is(status string) bool { return i.Status() == status }

// Parked reports whether the issue stands at a park.
func (i Issue) Parked() bool { return strings.HasPrefix(i.Status(), ParkPrefix) }

// ParkKind is the park's kind, "" when not parked.
func (i Issue) ParkKind() string {
	if !i.Parked() {
		return ""
	}
	return i.Status()[len(ParkPrefix):]
}

// IsBlockingPark reports a failure park, the one a person diagnoses.
func (i Issue) IsBlockingPark() bool { return i.Status() == StatusNeedsHumanFailure }

// Origin is the origin label the issue wears, "" for none.
func (i Issue) Origin() string {
	for _, n := range i.Labels {
		if contains(OriginLabels, n) {
			return n
		}
	}
	return ""
}

// SpellingsOf is every label that means status: what a transition out of
// it clears. Leaving a park clears every park spelling.
func SpellingsOf(status string) []string {
	if strings.HasPrefix(status, ParkPrefix) {
		out := append([]string{}, ParkStatuses...)
		for _, k := range ParkKinds {
			out = append(out, "task:needs-human-"+k)
		}
		return append(out, NeedsHuman)
	}
	out := []string{status}
	for _, l := range legacyStatus {
		if l.to == status {
			out = append(out, l.from)
		}
	}
	return out
}

// TriageLabelFor is the park a kind word names; anything else is failure.
func TriageLabelFor(kind string) string {
	if contains(ParkKinds, kind) {
		return ParkPrefix + kind
	}
	return StatusNeedsHumanFailure
}

// Outcome is the one outcome the labels carry: done, delivered, obsolete
// or "".
func (i Issue) Outcome() string {
	worn := StatusesOn(i.Labels)
	if contains(worn, StatusDone) {
		return "done"
	}
	if contains(worn, StatusRejected) {
		return "obsolete"
	}
	if i.HasLabel(OutcomeDelivered) {
		return "delivered"
	}
	return ""
}

// renamedPacks maps a canon pack's retired id to today's, for a title
// filed before the rename.
var renamedPacks = map[string]string{
	"barriers":       "basics",
	"tidy-repo":      "basics",
	"static-website": "public-website",
}

// CanonicalPackID is a pack id as it resolves today.
func CanonicalPackID(id string) string {
	if to, ok := renamedPacks[id]; ok {
		return to
	}
	return id
}

// Title is a work item's title, parsed. Qualifier is "" for none.
type Title struct {
	Pack      string `json:"pack"`
	Task      string `json:"task"`
	Qualifier string `json:"qualifier,omitempty"`
}

// String is the title as filed.
func (t Title) String() string {
	s := WorkPrefix + " " + t.Pack + "/" + t.Task
	if t.Qualifier != "" {
		s += " " + t.Qualifier
	}
	return s
}

// ID is the task's <pack>/<task>.
func (t Title) ID() string { return t.Pack + "/" + t.Task }

var titleRE = regexp.MustCompile(`^\[claudinite-work\]\s+([^/\s]+)/([^/\s]+)(?:\s+(\S.*))?$`)

// ParseTitle parses a work item's title; false for any other title.
func ParseTitle(title string) (Title, bool) {
	m := titleRE.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil {
		return Title{}, false
	}
	return Title{Pack: CanonicalPackID(m[1]), Task: m[2], Qualifier: strings.TrimSpace(m[3])}, true
}

var (
	packTaskPathRE    = regexp.MustCompile(`^(?:\.claudinite/shared/)?packs/([^/]+)/tasks/([^/]+)/[^/]+$`)
	builtInTaskPathRE = regexp.MustCompile(`^(?:\.claudinite/shared/)?(?:engine/scheduler|packs/claudinite-tasks)/queue/tasks/([^/]+)/[^/]+$`)
	// BuiltInPublicTaskPathRE is the built-in request task's spec, the path
	// new items name.
	BuiltInPublicTaskPathRE = regexp.MustCompile(`^(?:\.claudinite/(?:shared|local)/)?packs/claudinite-tasks/public/(implement-request)\.md$`)
)

// BuiltInPack is the id the engine's own tasks are titled with.
const BuiltInPack = "engine"

// TaskIDFromPath is the <pack>/<task> a worker path names, false when it
// names none.
func TaskIDFromPath(path string) (Title, bool) {
	if m := packTaskPathRE.FindStringSubmatch(path); m != nil {
		return Title{Pack: CanonicalPackID(m[1]), Task: m[2]}, true
	}
	if m := builtInTaskPathRE.FindStringSubmatch(path); m != nil {
		return Title{Pack: BuiltInPack, Task: m[1]}, true
	}
	if m := BuiltInPublicTaskPathRE.FindStringSubmatch(path); m != nil {
		return Title{Pack: BuiltInPack, Task: m[1]}, true
	}
	return Title{}, false
}

// TaskOf is the task an item names: its title, or the worker path its
// machine block names for a marked issue.
func (i Issue) TaskOf() (Title, bool) {
	if t, ok := ParseTitle(i.Title); ok {
		return t, true
	}
	return TaskIDFromPath(ParseBody(i.Body).TaskPath)
}

// Scheduled is whether a task is asked by the scheduler at HEAD: Yes, No,
// or Unknown for a task the repo no longer carries.
type Scheduled int

const (
	Unknown Scheduled = iota
	No
	Yes
)

// IsStandingItem reports a task's standing item: an unqualified title
// naming a task the scheduler asks.
func IsStandingItem(title string, scheduled Scheduled) bool {
	t, ok := ParseTitle(title)
	return ok && t.Qualifier == "" && scheduled == Yes
}

var (
	ruleNameRE    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	pathSegmentRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

func policyName(name string) bool {
	if !strings.HasPrefix(name, "under:") {
		return ruleNameRE.MatchString(name)
	}
	dir := strings.TrimRight(name[len("under:"):], "/")
	for _, seg := range strings.Split(dir, "/") {
		if !pathSegmentRE.MatchString(seg) || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// PolicyFieldValue fences a Merge/Automerge field to what the policy
// engine reads: the canonical expression, or "" for anything else.
func PolicyFieldValue(raw string) string {
	value := strings.TrimSpace(raw)
	switch strings.ToLower(value) {
	case "if-narrow", "yes", "true":
		return MergeIfNarrow
	case "anything":
		return "anything"
	}
	var canon []string
	anyAllow := false
	for _, term := range strings.Split(value, ";") {
		t := strings.TrimSpace(term)
		prefix := ""
		if strings.HasPrefix(t, "reject:") {
			prefix = "reject:"
		} else {
			anyAllow = true
		}
		var names []string
		for _, n := range strings.Split(t[len(prefix):], "&&") {
			n = strings.TrimSpace(n)
			if !policyName(n) || n == "nothing" {
				return ""
			}
			names = append(names, n)
		}
		canon = append(canon, prefix+strings.Join(names, "&&"))
	}
	if !anyAllow {
		return ""
	}
	return strings.Join(canon, ";")
}

var blockRE = regexp.MustCompile(`(?s)<!-- claudinite-item -->\n?(.*?)\n?<!-- /claudinite-item -->`)

// MachineBlockOf is the machine's half of a body, false where there is no
// block.
func MachineBlockOf(body string) (string, bool) {
	m := blockRE.FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// ItemFieldText is the text fields are read from: the block, or the whole
// body.
func ItemFieldText(body string) string {
	if b, ok := MachineBlockOf(body); ok {
		return b
	}
	return body
}

func trimRightSpace(s string) string { return strings.TrimRight(s, " \t\r\n\f\v") }

// WithMachineBlock replaces the block, or appends one after a blank line.
func WithMachineBlock(body, block string) string {
	wrapped := MachineBlockStart + "\n" + trimRightSpace(block) + "\n" + MachineBlockEnd
	if loc := blockRE.FindStringIndex(body); loc != nil {
		return body[:loc[0]] + wrapped + body[loc[1]:]
	}
	return trimRightSpace(body) + "\n\n" + wrapped + "\n"
}

// EditItemBody applies edit to whichever half is the machine's.
func EditItemBody(body string, edit func(string) string) string {
	if b, ok := MachineBlockOf(body); ok {
		return WithMachineBlock(body, edit(b))
	}
	return edit(body)
}

// HumanTextOf is everything the machine block is not.
func HumanTextOf(body string) string {
	if loc := blockRE.FindStringIndex(body); loc != nil {
		body = body[:loc[0]] + body[loc[1]:]
	}
	return strings.TrimSpace(body)
}

var (
	notBeforeRE    = regexp.MustCompile(`(?m)^Not-before:[ \t]*(.*)$`)
	blockedByRE    = regexp.MustCompile(`(?m)^Blocked-by:[ \t]*(.*)$`)
	requestRE      = regexp.MustCompile(`(?m)^Request:[ \t]*#?(\d+)`)
	modelRE        = regexp.MustCompile(`(?m)^Model:[ \t]*(\S+)`)
	mergeRE        = regexp.MustCompile(`(?m)^Merge:[ \t]*(\S+)`)
	endsWhenRE     = regexp.MustCompile(`(?m)^Ends-when:[ \t]*#(\d+)[ \t]+(\S+)[ \t]*$`)
	targetBranchRE = regexp.MustCompile(`(?m)^Target-branch:[ \t]*(\S+)[ \t]*$`)
	targetPRRE     = regexp.MustCompile(`(?m)^Target-pr:[ \t]*#?(\d+)[ \t]*$`)
	supersedesRE   = regexp.MustCompile(`(?m)^Supersedes:[ \t]*(.*)$`)
	wokenRE        = regexp.MustCompile(`(?m)^Woken:[ \t]*(\S+)[ \t]*$`)
	numberRefRE    = regexp.MustCompile(`#(\d+)`)
	taskFieldRE    = regexp.MustCompile(`(?m)^Task:[ \t]*(\S+)`)
	automergeRE    = regexp.MustCompile(`(?m)^Automerge:[ \t]*(.+?)[ \t]*$`)
	taskIDRE       = regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)
)

// BodySpec is what a work item body is built from.
type BodySpec struct {
	TaskPath  string
	NotBefore string
	BlockedBy []int
	Context   []string
	Delivered []string
	Reason    string
	Request   int
	Model     string
	Merge     string
	Woken     string
}

func refs(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = "#" + strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// Body builds a work item body: the task path first, the fields, the
// instruction, then the sections.
func Body(b BodySpec) string {
	lines := []string{b.TaskPath, ""}
	var fields []string
	if b.NotBefore != "" {
		fields = append(fields, NotBeforeField+": "+b.NotBefore)
	}
	if len(b.BlockedBy) > 0 {
		fields = append(fields, BlockedByField+": "+refs(b.BlockedBy))
	}
	if b.Request != 0 {
		fields = append(fields, RequestField+": #"+strconv.Itoa(b.Request))
	}
	if b.Model != "" {
		fields = append(fields, ModelField+": "+b.Model)
	}
	if b.Merge != "" {
		fields = append(fields, MergeField+": "+b.Merge)
	}
	if b.Woken != "" {
		fields = append(fields, WokenField+": "+b.Woken)
	}
	if len(fields) > 0 {
		lines = append(append(lines, fields...), "")
	}
	lines = append(lines, "Execute the Claudinite task above.")
	if len(b.Context) > 0 {
		lines = append(lines, "The Context section below is binding scope — do not re-decide it.", "", "### Context")
		for _, c := range b.Context {
			lines = append(lines, "- "+c)
		}
	}
	if b.Reason != "" {
		lines = append(lines, "", "### Why the agent is here", "", "- "+b.Reason)
	}
	if len(b.Delivered) > 0 {
		lines = append(lines, "", "### "+DeliveredHeading, "")
		for _, d := range b.Delivered {
			lines = append(lines, "- "+d)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// ParseBlockedBy is the Blocked-by numbers a body names.
func ParseBlockedBy(body string) []int {
	m := blockedByRE.FindStringSubmatch(body)
	if m == nil {
		return []int{}
	}
	return numbers(m[1])
}

func numbers(s string) []int {
	out := []int{}
	for _, r := range numberRefRE.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(r[1])
		out = append(out, n)
	}
	return out
}

// Fields are the facts a body carries. A nil pointer is absent, never a
// default.
type Fields struct {
	TaskPath     *string `json:"taskPath"`
	NotBefore    *string `json:"notBefore"`
	BlockedBy    []int   `json:"blockedBy"`
	Request      *int    `json:"request"`
	Model        *string `json:"model"`
	Merge        *string `json:"merge"`
	EndsWhen     *int    `json:"endsWhen"`
	TargetBranch *string `json:"targetBranch"`
	TargetPR     *int    `json:"targetPr"`
	Supersedes   []int   `json:"supersedes"`
	Woken        *string `json:"woken"`
}

func strp(s string) *string { return &s }
func intp(n int) *int       { return &n }

// Str is a nullable string's value, "" when absent.
func Str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Int is a nullable number's value, 0 when absent.
func Int(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ParsedBody is the body's fields with the absent ones read as zero
// values, for the callers that do not need the distinction.
type ParsedBody struct {
	TaskPath, NotBefore, Model, Merge, TargetBranch, Woken string
	BlockedBy, Supersedes                                []int
	Request, EndsWhen, TargetPR                          int
}

// ParseBody parses a body's fields with absence collapsed.
func ParseBody(body string) ParsedBody {
	f := ParseFields(body)
	return ParsedBody{
		TaskPath: Str(f.TaskPath), NotBefore: Str(f.NotBefore), Model: Str(f.Model), Merge: Str(f.Merge),
		TargetBranch: Str(f.TargetBranch), Woken: Str(f.Woken), BlockedBy: f.BlockedBy, Supersedes: f.Supersedes,
		Request: Int(f.Request), EndsWhen: Int(f.EndsWhen), TargetPR: Int(f.TargetPR),
	}
}

// ParseFields parses a body back into the facts the stages read.
func ParseFields(body string) Fields {
	text := ItemFieldText(body)
	var f Fields
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			f.TaskPath = strp(l)
			break
		}
	}
	if m := notBeforeRE.FindStringSubmatch(text); m != nil {
		if v := strings.TrimSpace(m[1]); v != "" {
			f.NotBefore = strp(v)
		}
	}
	f.BlockedBy = ParseBlockedBy(text)
	if m := requestRE.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		f.Request = intp(n)
	}
	if m := modelRE.FindStringSubmatch(text); m != nil && contains(RequestModels, m[1]) {
		f.Model = strp(m[1])
	}
	if m := mergeRE.FindStringSubmatch(text); m != nil {
		if v := PolicyFieldValue(m[1]); v != "" {
			f.Merge = strp(v)
		}
	}
	if m := endsWhenRE.FindStringSubmatch(text); m != nil && m[2] == EndsWhenClosed {
		n, _ := strconv.Atoi(m[1])
		f.EndsWhen = intp(n)
	}
	if m := targetBranchRE.FindStringSubmatch(text); m != nil {
		f.TargetBranch = strp(m[1])
	}
	if m := targetPRRE.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		f.TargetPR = intp(n)
	}
	f.Supersedes = []int{}
	if m := supersedesRE.FindStringSubmatch(text); m != nil {
		f.Supersedes = numbers(m[1])
	}
	if m := wokenRE.FindStringSubmatch(text); m != nil {
		f.Woken = strp(m[1])
	}
	return f
}

// Facts are an item's facts as a precondition term sees them.
type Facts struct {
	Fields
	Number int  `json:"number"`
	IsWoken bool `json:"wokenFlag"`
}

// ItemFacts are the body's fields plus the number and whether somebody
// created or woke the item: anything but the scheduler's own ask.
func ItemFacts(i Issue) Facts {
	f := ParseFields(i.Body)
	t, ok := ParseTitle(i.Title)
	schedulesOwn := ok && t.Qualifier == "" && !contains(AskedForOrigins, i.Origin())
	return Facts{Fields: f, Number: i.Number, IsWoken: f.Woken != nil || !schedulesOwn}
}

// RequestFields are what a marked issue asks for.
type RequestFields struct {
	Task      string `json:"task"`
	Model     string `json:"model"`
	Merge     string `json:"merge"`
	BlockedBy []int  `json:"blockedBy"`
	NotBefore string `json:"notBefore"`
	Ungated   bool   `json:"ungated"`
}

// ParseRequestFields reads a marked issue's parameters from the person's
// own text; the three behaviour-defining fields only when gated (the
// author holds push access).
func ParseRequestFields(body string, gated bool) RequestFields {
	text := HumanTextOf(body)
	first := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(text); m != nil {
			return m[1]
		}
		return ""
	}
	task, model, automerge := first(taskFieldRE), first(modelRE), first(automergeRE)
	out := RequestFields{BlockedBy: ParseBlockedBy(text)}
	if m := notBeforeRE.FindStringSubmatch(text); m != nil {
		out.NotBefore = strings.TrimSpace(m[1])
	}
	if !gated {
		out.Ungated = task != "" || model != "" || automerge != ""
		return out
	}
	if taskIDRE.MatchString(task) {
		out.Task = task
	}
	if contains(RequestModels, model) {
		out.Model = model
	}
	if automerge != "" {
		out.Merge = PolicyFieldValue(automerge)
	}
	return out
}

func sectionLines(body, heading string) []string {
	lines := strings.Split(body, "\n")
	at := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "### "+heading {
			at = i
			break
		}
	}
	out := []string{}
	if at < 0 {
		return out
	}
	bullet := regexp.MustCompile(`^-[ \t]+(.*)$`)
	for _, l := range lines[at+1:] {
		if strings.HasPrefix(l, "### ") {
			break
		}
		if m := bullet.FindStringSubmatch(l); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

// ContextLines are the item's own ### Context bullets.
func ContextLines(body string) []string { return sectionLines(body, "Context") }

// ProgressLines are the item's own ### Progress bullets.
func ProgressLines(body string) []string { return sectionLines(body, ProgressHeading) }

// LastVerdictLines is the section a no-go roll keeps.
func LastVerdictLines(at, reason, until string) []string {
	lines := []string{at + " — the precondition declined: " + reason}
	if until != "" {
		lines = append(lines, "Asked again at "+until+".")
	}
	return lines
}

// LastVerdict is a roll's record read back.
type LastVerdict struct {
	At, Reason, Until string
}

var (
	lastVerdictRE = regexp.MustCompile(`(?s)^(.*?) — the precondition declined: (.*)$`)
	askedAgainRE  = regexp.MustCompile(`^Asked again at (.*?)\.?$`)
)

// ParseLastVerdict reads back what LastVerdictLines wrote; false on a
// body that never rolled.
func ParseLastVerdict(body string) (LastVerdict, bool) {
	lines := sectionLines(body, LastVerdictHead)
	if len(lines) == 0 {
		return LastVerdict{}, false
	}
	m := lastVerdictRE.FindStringSubmatch(lines[0])
	if m == nil {
		return LastVerdict{}, false
	}
	v := LastVerdict{At: m[1], Reason: m[2]}
	for _, l := range lines {
		if a := askedAgainRE.FindStringSubmatch(l); a != nil {
			v.Until = a[1]
			break
		}
	}
	return v, true
}

// MergeContext folds Context groups together, order kept, exact
// duplicates and blanks dropped.
func MergeContext(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		for _, l := range g {
			if strings.TrimSpace(l) != "" && !contains(out, l) {
				out = append(out, l)
			}
		}
	}
	return out
}

// insertUnderPath inserts lines after the first non-blank line, behind a
// blank line.
func insertUnderPath(text string, add ...string) string {
	lines := strings.Split(text, "\n")
	at := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			at = i
			break
		}
	}
	if at < 0 {
		return strings.Join(add, "\n") + "\n"
	}
	out := append(append([]string{}, lines[:at+1]...), "")
	out = append(append(out, add...), lines[at+1:]...)
	return strings.Join(out, "\n")
}

func replaceFirst(re *regexp.Regexp, text, repl string) string {
	loc := re.FindStringIndex(text)
	if loc == nil {
		return text
	}
	return text[:loc[0]] + repl + text[loc[1]:]
}

var notBeforeLineRE = regexp.MustCompile(`(?m)^Not-before:[ \t]*.*\n?`)

// WithNotBefore stamps Not-before in place or under the task path; ""
// clears it.
func WithNotBefore(body, iso string) string {
	if notBeforeRE.MatchString(body) {
		if iso != "" {
			return replaceFirst(notBeforeRE, body, NotBeforeField+": "+iso)
		}
		return replaceFirst(notBeforeLineRE, body, "")
	}
	if iso == "" {
		return body
	}
	return insertUnderPath(body, NotBeforeField+": "+iso)
}

// WithWoken stamps Woken, replacing an earlier instant.
func WithWoken(body, iso string) string {
	return EditItemBody(body, func(text string) string {
		if wokenRE.MatchString(text) {
			return replaceFirst(wokenRE, text, WokenField+": "+iso)
		}
		return insertUnderPath(text, WokenField+": "+iso)
	})
}

// WithEndsWhen stamps a park's end condition, replacing an earlier one.
func WithEndsWhen(body string, number int) string {
	field := EndsWhenField + ": #" + strconv.Itoa(number) + " " + EndsWhenClosed
	if endsWhenRE.MatchString(body) {
		return replaceFirst(endsWhenRE, body, field)
	}
	return insertUnderPath(body, field)
}

// Target is which branch and pull request a run works on.
type Target struct {
	Mode       string `json:"mode"`
	Branch     string `json:"branch,omitempty"`
	PR         int    `json:"pr,omitempty"`
	Supersedes []int  `json:"supersedes,omitempty"`
}

var (
	targetBranchLineRE = regexp.MustCompile(`(?m)^Target-branch:[ \t]*.*\n?`)
	targetPRLineRE     = regexp.MustCompile(`(?m)^Target-pr:[ \t]*.*\n?`)
	supersedesLineRE   = regexp.MustCompile(`(?m)^Supersedes:[ \t]*.*\n?`)
)

// WithTarget stamps the target's lines in the machine's half, replacing
// what an earlier resolution left.
func WithTarget(body string, t Target) string {
	return EditItemBody(body, func(text string) string {
		var wanted []string
		if t.Branch != "" {
			wanted = append(wanted, TargetBranchField+": "+t.Branch)
		}
		if t.PR != 0 && t.Mode == "amend" {
			wanted = append(wanted, TargetPRField+": #"+strconv.Itoa(t.PR))
		}
		if len(t.Supersedes) > 0 {
			wanted = append(wanted, SupersedesField+": "+refs(t.Supersedes))
		}
		stripped := targetBranchLineRE.ReplaceAllString(text, "")
		stripped = targetPRLineRE.ReplaceAllString(stripped, "")
		stripped = supersedesLineRE.ReplaceAllString(stripped, "")
		if len(wanted) == 0 {
			return stripped
		}
		return insertUnderPath(stripped, wanted...)
	})
}

// WithSection sets a section, replacing one of the same heading in place
// or appending it.
func WithSection(body, heading string, lines []string) string {
	if len(lines) == 0 {
		return body
	}
	text := trimRightSpace(body)
	section := []string{"### " + heading, ""}
	for _, l := range lines {
		section = append(section, "- "+l)
	}
	existing := strings.Split(text, "\n")
	at := -1
	for i, l := range existing {
		if strings.TrimSpace(l) == "### "+heading {
			at = i
			break
		}
	}
	if at < 0 {
		return text + "\n\n" + strings.Join(section, "\n") + "\n"
	}
	after := -1
	for i := at + 1; i < len(existing); i++ {
		if strings.HasPrefix(existing[i], "### ") {
			after = i
			break
		}
	}
	out := append(append([]string{}, existing[:at]...), section...)
	if after >= 0 {
		out = append(append(out, ""), existing[after:]...)
	}
	return strings.Join(out, "\n") + "\n"
}

// IsQueueItem reports an issue that is an item: a filed work item, an
// adopted issue's machine block, or the ad-hoc mark beside a status.
func (i Issue) IsQueueItem() bool {
	if strings.HasPrefix(i.Title, WorkPrefix) {
		return true
	}
	if _, ok := MachineBlockOf(i.Body); ok {
		return true
	}
	return i.HasLabel(OriginAdHoc) && i.Status() != ""
}

var taskTrailerRE = regexp.MustCompile(`(?m)^Claudinite-Task:[ \t]*(\S+)[ \t]*$`)

// WithTaskTrailer appends "Claudinite-Task: <id>" after a blank line,
// unless the message already carries one or id is "".
func WithTaskTrailer(message, id string) string {
	if id == "" || taskTrailerRE.MatchString(message) {
		return message
	}
	return strings.TrimRight(message, " \t\r\n") + "\n\n" + TaskTrailer + ": " + id + "\n"
}

// TaskFromMessage is the task a commit message's trailer names, "" when
// none does.
func TaskFromMessage(message string) string {
	if m := taskTrailerRE.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	return ""
}
