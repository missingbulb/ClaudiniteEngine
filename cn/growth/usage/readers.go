package usage

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
)

// Reader is the REST surface the folds read. JSON answers the parsed body,
// nil for a status other than 200 or a body that does not parse; Text
// answers a log the same way. Each errs only when GitHub was not reached.
type Reader interface {
	JSON(path string) (any, error)
	Text(path string) (*string, error)
}

// WatchedWorkflows are the two workflows whose runs the folds count, under
// the counter key each is counted as.
var WatchedWorkflows = []struct{ Workflow, File string }{
	{"scheduler", workitem.SchedulerWorkflowFile},
	{"executor", workitem.ExecutorWorkflowFile},
}

// FirstFoldLookbackDays is how far back the run listing looks with no mark
// yet: the hour tier's width. FirstReadLookbackDays is the same for the
// closed items and merged PRs: the day tier's.
const (
	FirstFoldLookbackDays = 3
	FirstReadLookbackDays = 30
)

// LookbackFrom is now less days, as an ISO instant.
func LookbackFrom(now string, days float64) string {
	return isoString(parseDate(now) - days*86400000)
}

// encodeURIComponent is the JS function: every byte but the unreserved
// marks percent-encoded.
func encodeURIComponent(s string) string {
	const keep = "-_.!~*'()"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%" + strings.ToUpper(string("0123456789abcdef"[c>>4])+string("0123456789abcdef"[c&15])))
	}
	return b.String()
}

// markOr is a read's lower bound: its mark, else the first read's
// lookback from now.
func markOr(since any, now string, days float64) any {
	if truthy(since) {
		return since
	}
	return LookbackFrom(now, days)
}

func arrayOf(v any) ([]any, bool) {
	a, ok := v.([]any)
	return a, ok
}

func orEmptyString(v any, ok bool) string {
	if !ok || v == nil {
		return ""
	}
	return jsString(v)
}

// ReadWorkflowRuns is one workflow's completed runs started strictly after
// since, newest first as the listing answers them.
func ReadWorkflowRuns(r Reader, repo, file string, since any, maxPages int) ([]Run, error) {
	var runs []Run
	for page := 1; page <= maxPages; page++ {
		p := "/repos/" + repo + "/actions/workflows/" + file + "/runs?status=completed&per_page=100&page=" + itoa(page) +
			"&created=%3E%3D" + encodeURIComponent(sliceUnits(jsString(since), 0, 10))
		json, err := r.JSON(p)
		if err != nil {
			return nil, err
		}
		batch, ok := arrayOf(nullish(path(json, "workflow_runs")))
		if !ok || len(batch) == 0 {
			break
		}
		for _, run := range batch {
			if run == nil {
				return nil, typeError("id", run)
			}
			at, _ := propOf(run, "run_started_at")
			if !truthy(at) {
				at, _ = propOf(run, "created_at")
			}
			id, _ := propOf(run, "id")
			if !truthy(at) || !truthy(id) {
				continue
			}
			if jsAtMost(at, since) {
				continue
			}
			started := jsString(at)
			conclusion, _ := propOf(run, "conclusion")
			runs = append(runs, Run{ID: id, StartedAt: started, Conclusion: conclusion})
		}
		if len(batch) < 100 {
			break
		}
	}
	return runs, nil
}

// RunsRead is the run listings' answer: the runs oldest first across both
// workflows, the new mark (the old one when nothing was read), and why the
// listings failed, "" when they did not.
type RunsRead struct {
	Runs      []Run
	Watermark any
	Error     string
}

// ReadRuns lists both workflows' completed runs past since, or the first
// fold's lookback from now when there is no mark.
func ReadRuns(r Reader, repo string, since any, now string) RunsRead {
	from := markOr(since, now, FirstFoldLookbackDays)
	runs := []Run{}
	for _, w := range WatchedWorkflows {
		got, err := ReadWorkflowRuns(r, repo, w.File, from, 3)
		if err != nil {
			return RunsRead{Runs: []Run{}, Watermark: since, Error: "the workflow run listings could not be read"}
		}
		for _, run := range got {
			run.Workflow = w.Workflow
			runs = append(runs, run)
		}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		a, b := runs[i], runs[j]
		if a.StartedAt != b.StartedAt {
			return jsLess(a.StartedAt, b.StartedAt)
		}
		return toNumber(a.ID)-toNumber(b.ID) < 0
	})
	if len(runs) == 0 {
		return RunsRead{Runs: runs, Watermark: since}
	}
	return RunsRead{Runs: runs, Watermark: runs[len(runs)-1].StartedAt}
}

// The run cost read's bounds: the scheduler ticks whose log is read, the
// job logs read within one, and the runaway guard on per-run reads.
const (
	SchedulerRunsPerFold = 2
	JobLogsPerRun        = 2
	MaxRunReads          = 40
)

// BilledMinutes is a run's jobs' wall time, each rounded up to a whole
// minute and summed; nil when no job could be measured.
func BilledMinutes(jobs []any) *float64 {
	var minutes *float64
	for _, job := range jobs {
		from := parseDate(orEmptyString(propOf(job, "started_at")))
		to := parseDate(orEmptyString(propOf(job, "completed_at")))
		if math.IsNaN(from) || math.IsNaN(to) || to < from {
			continue
		}
		m := math.Ceil((to - from) / 60000)
		if minutes != nil {
			m += *minutes
		}
		minutes = &m
	}
	return minutes
}

// ReadJobs is one run's jobs, nil when the listing could not be read.
func ReadJobs(r Reader, repo string, runID any) ([]any, error) {
	json, err := r.JSON("/repos/" + repo + "/actions/runs/" + jsString(runID) + "/jobs?per_page=100")
	if err != nil {
		return nil, err
	}
	jobs, ok := arrayOf(nullish(path(json, "jobs")))
	if !ok {
		return nil, nil
	}
	return jobs, nil
}

func ranJob(job any) bool {
	c, ok := propOf(job, "conclusion")
	return ok && c != nil && c != "skipped"
}

func runCostOf(c queue.RunCost) RunCost {
	out := RunCost{RunID: c.RunID, PhaseMs: c.PhaseMs}
	if c.APICalls != nil {
		out.APICalls = f64(float64(*c.APICalls))
	}
	return out
}

// ParseRunCosts is every cost record in a text, in order.
func ParseRunCosts(text string) []RunCost {
	out := []RunCost{}
	for _, l := range strings.Split(text, "\n") {
		if c, ok := queue.ParseRunCost(l); ok {
			out = append(out, runCostOf(c))
		}
	}
	return out
}

// ReadCostFromLog is the last cost record in the first of a tick's jobs'
// logs that carries one, reading at most budget logs; it answers the
// reads it made.
func ReadCostFromLog(r Reader, repo string, jobs []any, budget int) (*RunCost, int, error) {
	reads := 0
	for _, job := range jobs {
		if !ranJob(job) {
			continue
		}
		if reads >= budget {
			break
		}
		reads++
		id, _ := propOf(job, "id")
		text, err := r.Text("/repos/" + repo + "/actions/jobs/" + jsString(id) + "/logs")
		if err != nil {
			return nil, reads, err
		}
		body := ""
		if text != nil {
			body = *text
		}
		if found := ParseRunCosts(body); len(found) > 0 {
			return &found[len(found)-1], reads, nil
		}
	}
	return nil, reads, nil
}

// RunCostsRead is the machinery half's run read: each run with its jobs
// and cost, the newest run start read in full, and whether the cap
// stopped it.
type RunCostsRead struct {
	Runs      []TasksRun
	Watermark any
	Error     string
	Truncated bool
}

// ReadRunCosts lists the runs past since and reads each one's jobs and, for
// the first scheduler ticks, its log.
func ReadRunCosts(r Reader, repo string, since any, now string, maxRunReads int) (RunCostsRead, error) {
	listed := ReadRuns(r, repo, since, now)
	if listed.Error != "" {
		return RunCostsRead{Runs: []TasksRun{}, Watermark: listed.Watermark, Error: listed.Error}, nil
	}
	out := []TasksRun{}
	reads, logsRead, truncated := 0, 0, false
	watermark := since
	for _, run := range listed.Runs {
		if reads >= maxRunReads {
			truncated = true
			break
		}
		reads++
		jobs, err := ReadJobs(r, repo, run.ID)
		if err != nil {
			return RunCostsRead{}, err
		}
		var cost *RunCost
		if run.Workflow == "scheduler" && jobs != nil && logsRead < SchedulerRunsPerFold {
			logsRead++
			c, n, err := ReadCostFromLog(r, repo, jobs, JobLogsPerRun)
			if err != nil {
				return RunCostsRead{}, err
			}
			reads += n
			cost = c
		}
		row := TasksRun{ID: run.ID, StartedAt: run.StartedAt, Workflow: run.Workflow, Conclusion: run.Conclusion, Cost: cost}
		if jobs != nil {
			row.Jobs = f64(float64(len(jobs)))
			row.MinutesBilled = BilledMinutes(jobs)
		}
		out = append(out, row)
		watermark = run.StartedAt
	}
	if watermark == nil {
		watermark = listed.Watermark
	}
	return RunCostsRead{Runs: out, Watermark: watermark, Truncated: truncated}, nil
}

// issueOf is a listed issue as the work-item grammar reads it.
func issueOf(v any) workitem.Issue {
	var i workitem.Issue
	i.Title = orEmptyString(propOf(v, "title"))
	i.Body = orEmptyString(propOf(v, "body"))
	i.State = orEmptyString(propOf(v, "state"))
	labels, _ := propOf(v, "labels")
	list, _ := arrayOf(labels)
	for _, l := range list {
		name := l
		if _, isString := l.(string); !isString {
			name, _ = propOf(l, "name")
		}
		if truthy(name) {
			i.Labels = append(i.Labels, jsString(name))
		}
	}
	return i
}

// legacyParkRE is the two-label era's park sub-label.
// @legacy-tolerance advisory:none retire:#1913
var legacyParkRE = regexp.MustCompile(`^task:needs-human-(.+)$`)

// ParkKindOf is the park kind a label names, "" for a label that is not a
// park; a kind no engine here knows reads as failure.
func ParkKindOf(label any) string {
	name := ""
	if label != nil {
		name = jsString(label)
	}
	if name == workitem.NeedsHuman {
		return "failure"
	}
	var kind string
	switch {
	case strings.HasPrefix(name, workitem.ParkPrefix):
		kind = name[len(workitem.ParkPrefix):]
	default:
		m := legacyParkRE.FindStringSubmatch(name)
		if m == nil {
			return ""
		}
		kind = m[1]
	}
	if has(workitem.ParkKinds, kind) {
		return kind
	}
	return "failure"
}

// parksOf is the park kinds a list of label events applied, deduped in
// the order first applied.
func parksOf(events []any) []string {
	kinds := []string{}
	for _, e := range events {
		if ev, _ := propOf(e, "event"); ev != "labeled" {
			continue
		}
		name, _ := path(e, "label", "name")
		if k := ParkKindOf(name); k != "" && !has(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

// ReadParks is the park kinds one item collected, off its event listing;
// nil when that listing could not be read.
func ReadParks(r Reader, repo string, number any) []string {
	events, err := r.JSON("/repos/" + repo + "/issues/" + jsString(number) + "/events?per_page=100")
	list, ok := arrayOf(events)
	if err != nil || !ok {
		return nil
	}
	return parksOf(list)
}

// closedRecord is the part of a closed item both folds read alike, false
// when the item is not a fold's business.
func closedRecord(v any, since any) (date, closedAt string, task workitem.Title, outcome string, number any, ok bool) {
	issue := issueOf(v)
	if !issue.IsQueueItem() {
		return
	}
	at, _ := propOf(v, "closed_at")
	if !truthy(at) {
		return
	}
	closedAt = jsString(at)
	if truthy(since) && jsAtMost(at, since) {
		return
	}
	task, found := issue.TaskOf()
	if !found {
		return
	}
	outcome = issue.Outcome()
	if outcome == "" {
		outcome = "none"
	}
	n, _ := propOf(v, "number")
	return sliceUnits(closedAt, 0, 10), closedAt, task, outcome, n, true
}

// QueueRecordFor is one closed item as the session half counts it, false
// when it is not this fold's business. Parks stays nil for the caller.
func QueueRecordFor(v any, since any) (QueueRecord, bool) {
	date, closedAt, task, outcome, number, ok := closedRecord(v, since)
	if !ok {
		return QueueRecord{}, false
	}
	return QueueRecord{Date: date, ClosedAt: closedAt, Pack: task.Pack, Task: task.Task, Outcome: outcome, Number: number}, true
}

// readClosedListing pages the closed issues touched since from, handing
// each that is not a pull request to keep.
func readClosedListing(r Reader, repo, from string, maxPages int, keep func(any)) error {
	for page := 1; page <= maxPages; page++ {
		p := "/repos/" + repo + "/issues?state=closed&sort=updated&direction=desc&since=" + encodeURIComponent(from) +
			"&per_page=100&page=" + itoa(page)
		json, err := r.JSON(p)
		if err != nil {
			return err
		}
		batch, ok := arrayOf(json)
		if !ok || len(batch) == 0 {
			return nil
		}
		for _, issue := range batch {
			if pr, _ := propOf(issue, "pull_request"); truthy(pr) {
				continue
			}
			keep(issue)
		}
		if len(batch) < 100 {
			return nil
		}
	}
	return nil
}

// newestClosed is the latest close among the records, else since.
func newestClosed(closed []string, since any) any {
	if len(closed) == 0 {
		return since
	}
	sorted := jsSort(append([]string{}, closed...))
	return sorted[len(sorted)-1]
}

// QueueRead is the queue outcome read's answer.
type QueueRead struct {
	Records   []QueueRecord
	Watermark any
	Error     string
}

// ReadQueueOutcomes lists the work items that closed past since, each with
// the parks its event listing answers.
func ReadQueueOutcomes(r Reader, repo string, since any, now string) QueueRead {
	from := markOr(since, now, FirstReadLookbackDays)
	records := []QueueRecord{}
	err := readClosedListing(r, repo, jsString(from), 5, func(issue any) {
		if rec, ok := QueueRecordFor(issue, since); ok {
			records = append(records, rec)
		}
	})
	if err != nil {
		return QueueRead{Records: []QueueRecord{}, Watermark: since, Error: "the closed work items could not be listed"}
	}
	closed := make([]string, len(records))
	for i := range records {
		if records[i].Number != nil {
			records[i].Parks = ReadParks(r, repo, records[i].Number)
		}
		closed[i] = records[i].ClosedAt
	}
	return QueueRead{Records: records, Watermark: newestClosed(closed, since)}
}

// firstLabeledAt is when a label in names was first applied, nil when none
// was.
func firstLabeledAt(timeline []any, names []string) any {
	for _, e := range timeline {
		if ev, _ := propOf(e, "event"); ev != "labeled" {
			continue
		}
		name, _ := path(e, "label", "name")
		if has(names, orEmptyString(name, true)) {
			return nullish(propOf(e, "created_at"))
		}
	}
	return nil
}

// ParksIn is the park kinds a timeline applied, deduped.
func ParksIn(timeline []any) []string { return parksOf(timeline) }

// CostsIn is the cost records a timeline's comments carry.
func CostsIn(timeline []any) []RunCost {
	out := []RunCost{}
	for _, e := range timeline {
		if ev, _ := propOf(e, "event"); ev != "commented" {
			continue
		}
		body, _ := propOf(e, "body")
		out = append(out, ParseRunCosts(orEmptyString(body, true))...)
	}
	return out
}

func minutesBetween(from, to any) (float64, bool) {
	a := parseDate(orEmptyString(from, true))
	b := parseDate(orEmptyString(to, true))
	if math.IsNaN(a) || math.IsNaN(b) || b < a {
		return 0, false
	}
	return jsRound((b - a) / 60000), true
}

// LatencyOf is an item's four latencies in minutes, each absent where its
// far end never happened or is outside what this fold read.
func LatencyOf(item any, timeline []any, schedulerStarts []string) *Obj {
	createdAt := nullish(propOf(item, "created_at"))
	pickedAt := firstLabeledAt(timeline, workitem.SpellingsOf(workitem.StatusRunningExecutor))
	handedOffAt := firstLabeledAt(timeline, workitem.SpellingsOf(workitem.StatusRunningAgent))
	closedAt := nullish(propOf(item, "closed_at"))
	var tickAt any
	for _, s := range schedulerStarts {
		if later, _ := jsLessThan(createdAt, s); s == "" || !truthy(createdAt) || later {
			continue
		}
		if tickAt == nil || jsLess(tickAt.(string), s) {
			tickAt = s
		}
	}
	row := NewObj()
	for _, slot := range []struct {
		name     string
		from, to any
	}{
		{"tickToItemMinutes", tickAt, createdAt},
		{"itemToPickMinutes", createdAt, pickedAt},
		{"pickToHandOffMinutes", pickedAt, handedOffAt},
		{"handOffToConvergeMinutes", handedOffAt, closedAt},
	} {
		if slot.from == nil || slot.to == nil {
			continue
		}
		if m, ok := minutesBetween(slot.from, slot.to); ok {
			row.Set(slot.name, m)
		}
	}
	return row
}

// ClosedItemFor is one closed item as the machinery half counts it, false
// when it is not this fold's business; parks, latency and costs stay nil
// for the caller.
func ClosedItemFor(v any, since any) (ClosedItem, bool) {
	date, closedAt, task, outcome, number, ok := closedRecord(v, since)
	if !ok {
		return ClosedItem{}, false
	}
	return ClosedItem{Date: date, ClosedAt: closedAt, Pack: task.Pack, Task: task.Task, Outcome: outcome,
		Number: number, CreatedAt: nullish(propOf(v, "created_at"))}, true
}

// ReadTimeline is one item's timeline, nil when its first page could not
// be read.
func ReadTimeline(r Reader, repo string, number any, maxPages int) ([]any, error) {
	out := []any{}
	for page := 1; page <= maxPages; page++ {
		json, err := r.JSON("/repos/" + repo + "/issues/" + jsString(number) + "/timeline?per_page=100&page=" + itoa(page))
		if err != nil {
			return nil, err
		}
		batch, ok := arrayOf(json)
		if !ok || len(batch) == 0 {
			if page == 1 {
				return nil, nil
			}
			return out, nil
		}
		out = append(out, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

// ItemsRead is the closed item read's answer.
type ItemsRead struct {
	Records   []ClosedItem
	Watermark any
	Error     string
}

// ReadClosedItems lists the work items that closed past since, each with
// the parks, latencies and cost records its timeline answers.
func ReadClosedItems(r Reader, repo string, since any, now string, schedulerStarts []string) ItemsRead {
	from := markOr(since, now, FirstReadLookbackDays)
	type found struct {
		rec   ClosedItem
		issue any
	}
	var records []found
	err := readClosedListing(r, repo, jsString(from), 5, func(issue any) {
		if rec, ok := ClosedItemFor(issue, since); ok {
			records = append(records, found{rec, issue})
		}
	})
	if err != nil {
		return ItemsRead{Records: []ClosedItem{}, Watermark: since, Error: "the closed work items could not be listed"}
	}
	out := []ClosedItem{}
	closed := []string{}
	for _, f := range records {
		if f.rec.Number != nil {
			if timeline, err := ReadTimeline(r, repo, f.rec.Number, 2); err == nil && timeline != nil {
				f.rec.Parks = ParksIn(timeline)
				f.rec.Latency = LatencyOf(f.issue, timeline, schedulerStarts)
				f.rec.Costs = CostsIn(timeline)
			}
		}
		out = append(out, f.rec)
		closed = append(closed, f.rec.ClosedAt)
	}
	return ItemsRead{Records: out, Watermark: newestClosed(closed, since)}
}

// closesRE is the closing keyword GitHub acts on, the first match winning.
var closesRE = regexp.MustCompile(`(?:^|\n)[^\S\n]*(?:` + ci("closes") + `|` + ci("fixes") + `|` + ci("resolves") + `)[^\S\n]+#(\d+)\b`)

// ClosesIssueIn is the issue a PR body closes, nil for none.
func ClosesIssueIn(body any) *float64 {
	text := ""
	if body != nil {
		text = jsString(body)
	}
	m := closesRE.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	n := stringToNumber(m[1])
	if math.IsInf(n, 0) || math.IsNaN(n) || n <= 0 {
		return nil
	}
	return &n
}

// HoursBetween is a span in hours to one decimal; nil where either end is
// absent or they arrive in the wrong order.
func HoursBetween(from, to any) *float64 {
	if !truthy(from) || !truthy(to) {
		return nil
	}
	h := (parseDate(jsString(to)) - parseDate(jsString(from))) / 3600000
	if math.IsNaN(h) || math.IsInf(h, 0) || h < 0 {
		return nil
	}
	h = jsRound(h*10) / 10
	return &h
}

// MergedPr is one merged pull request the listing read.
type MergedPr struct {
	Number         any
	MergedAt       string
	CreatedAt      any
	ClosesIssue    *float64
	IssueCreatedAt any
}

// PrsRead is the merged PR read's answer.
type PrsRead struct {
	Prs       []MergedPr
	Watermark any
	Error     string
}

// ReadMergedPrs lists the pull requests merged past since, each with the
// creation time of the issue its body closes.
func ReadMergedPrs(r Reader, repo string, since any, now string) PrsRead {
	from := markOr(since, now, FirstReadLookbackDays)
	prs := []MergedPr{}
	fail := PrsRead{Prs: []MergedPr{}, Watermark: since, Error: "the merged pull requests could not be listed"}
	for page := 1; page <= 5; page++ {
		json, err := r.JSON("/repos/" + repo + "/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=" + itoa(page))
		if err != nil {
			return fail
		}
		batch, ok := arrayOf(json)
		if !ok || len(batch) == 0 {
			break
		}
		for _, pr := range batch {
			at := nullish(propOf(pr, "merged_at"))
			if !truthy(at) || jsAtMost(at, from) {
				continue
			}
			number, _ := propOf(pr, "number")
			body, _ := propOf(pr, "body")
			prs = append(prs, MergedPr{Number: number, MergedAt: jsString(at), CreatedAt: nullish(propOf(pr, "created_at")),
				ClosesIssue: ClosesIssueIn(body)})
		}
		if len(batch) < 100 {
			break
		}
	}
	merged := make([]string, len(prs))
	for i := range prs {
		merged[i] = prs[i].MergedAt
		if prs[i].ClosesIssue == nil {
			continue
		}
		if issue, err := r.JSON("/repos/" + repo + "/issues/" + jsString(*prs[i].ClosesIssue)); err == nil {
			prs[i].IssueCreatedAt = nullish(path(issue, "created_at"))
		}
	}
	return PrsRead{Prs: prs, Watermark: newestClosed(merged, since)}
}

// PrRecordsFrom joins the merged PRs with the capture files into day-row
// records: a PR's session is the earliest capture keyed to it, or to the
// issue it closes.
func PrRecordsFrom(prs []MergedPr, files []CaptureFile) []PrRecord {
	byPr, byIssue := map[float64]string{}, map[float64]string{}
	for _, f := range files {
		if f.Stamp == "" {
			continue
		}
		var into map[float64]string
		var key float64
		switch {
		case positive(f.PR):
			into, key = byPr, *f.PR
		case positive(f.Issue):
			into, key = byIssue, *f.Issue
		default:
			continue
		}
		if first, ok := into[key]; !ok || jsLess(f.Stamp, first) {
			into[key] = f.Stamp
		}
	}
	out := make([]PrRecord, len(prs))
	for i, pr := range prs {
		var started any
		if n, ok := pr.Number.(float64); ok {
			if s, ok := byPr[n]; ok {
				started = s
			}
		}
		if started == nil && pr.ClosesIssue != nil {
			if s, ok := byIssue[*pr.ClosesIssue]; ok {
				started = s
			}
		}
		out[i] = PrRecord{Date: sliceUnits(pr.MergedAt, 0, 10), Number: pr.Number,
			LeadHours: HoursBetween(pr.CreatedAt, pr.MergedAt), IssueLeadHours: HoursBetween(pr.IssueCreatedAt, pr.MergedAt),
			SessionToMerge: HoursBetween(started, pr.MergedAt)}
	}
	return out
}
