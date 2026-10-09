package usage

import (
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// fakeReader answers each path through answer and records it: "how many
// requests did this cost" is half of why the readers are shaped as they
// are.
type fakeReader struct {
	paths  []string
	answer func(path string) (any, error)
	logs   map[string]string
}

func (f *fakeReader) JSON(path string) (any, error) {
	f.paths = append(f.paths, path)
	return f.answer(path)
}

func (f *fakeReader) Text(path string) (*string, error) {
	f.paths = append(f.paths, path)
	m := regexp.MustCompile(`/actions/jobs/(\d+)/logs`).FindStringSubmatch(path)
	if m == nil {
		return nil, nil
	}
	s, ok := f.logs[m[1]]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (f *fakeReader) count(re string) int {
	n := 0
	for _, p := range f.paths {
		if regexp.MustCompile(re).MatchString(p) {
			n++
		}
	}
	return n
}

// routes answers a path by the first route whose fragment it contains,
// nil (a 404) for none.
func routes(t *testing.T, r map[string]string) *fakeReader {
	return &fakeReader{answer: func(path string) (any, error) {
		for frag, body := range r {
			if strings.Contains(path, frag) {
				return js(t, body), nil
			}
		}
		return nil, nil
	}}
}

func unreachable() *fakeReader {
	return &fakeReader{answer: func(string) (any, error) { return nil, errors.New("network") }}
}

func runJSON(id int, at, conclusion string) string {
	return `{"id": ` + strconv.Itoa(id) + `, "run_started_at": "` + at + `", "conclusion": "` + conclusion + `"}`
}

func runsJSON(runs ...string) string { return `{"workflow_runs": [` + strings.Join(runs, ",") + `]}` }

func startsOf(runs []Run) (out [][2]string) {
	for _, r := range runs {
		out = append(out, [2]string{r.Workflow, r.StartedAt})
	}
	return out
}

// --- the run listings ---------------------------------------------------------

func TestTheWatchedWorkflowsAreTheSchedulersAndTheExecutorsOwnFileNames(t *testing.T) {
	if WatchedWorkflows[0].File != workitem.SchedulerWorkflowFile || WatchedWorkflows[1].File != workitem.ExecutorWorkflowFile ||
		WatchedWorkflows[0].Workflow != "scheduler" || WatchedWorkflows[1].Workflow != "executor" || len(WatchedWorkflows) != 2 {
		t.Fatalf("%+v", WatchedWorkflows)
	}
}

func TestWorkflowRunsSkipsRunsAtOrBeforeTheWatermark(t *testing.T) {
	r := routes(t, map[string]string{workitem.SchedulerWorkflowFile: runsJSON(
		runJSON(3, "2026-08-21T11:00:00Z", "success"), runJSON(2, "2026-08-21T10:00:00Z", "success"), runJSON(1, "2026-08-21T09:00:00Z", "success"))})
	runs, err := ReadWorkflowRuns(r, "o/r", workitem.SchedulerWorkflowFile, "2026-08-21T09:00:00Z", 3)
	if err != nil || len(runs) != 2 || runs[0].ID != 3.0 || runs[1].ID != 2.0 {
		t.Fatalf("%+v %v", runs, err)
	}
	if runs[0] != (Run{ID: 3.0, StartedAt: "2026-08-21T11:00:00Z", Conclusion: "success"}) {
		t.Fatalf("%+v", runs[0])
	}
}

func TestReadRunsLabelsOrdersAndMovesTheMark(t *testing.T) {
	r := routes(t, map[string]string{
		workitem.SchedulerWorkflowFile: runsJSON(runJSON(3, "2026-08-21T11:00:00Z", "success"), runJSON(1, "2026-08-21T09:30:00Z", "success")),
		workitem.ExecutorWorkflowFile:  runsJSON(runJSON(2, "2026-08-21T10:15:00Z", "failure")),
	})
	out := ReadRuns(r, "o/r", "2026-08-21T09:00:00Z", "2026-08-21T11:30:00Z")
	want := [][2]string{{"scheduler", "2026-08-21T09:30:00Z"}, {"executor", "2026-08-21T10:15:00Z"}, {"scheduler", "2026-08-21T11:00:00Z"}}
	if !reflect.DeepEqual(startsOf(out.Runs), want) || out.Watermark != "2026-08-21T11:00:00Z" {
		t.Fatalf("%+v", out)
	}
	if len(r.paths) != 2 || r.count(`status=completed`) != 2 {
		t.Fatalf("one listing per workflow, completed runs only: %v", r.paths)
	}
}

func TestReadRunsLeavesTheWatermarkWhereItWasWhenNothingNewLanded(t *testing.T) {
	r := routes(t, map[string]string{workitem.SchedulerWorkflowFile: runsJSON(), workitem.ExecutorWorkflowFile: runsJSON()})
	out := ReadRuns(r, "o/r", "2026-08-21T09:00:00Z", "2026-08-21T11:30:00Z")
	if len(out.Runs) != 0 || out.Runs == nil || out.Watermark != "2026-08-21T09:00:00Z" {
		t.Fatalf("%+v", out)
	}
}

func TestAnUnreadableRunListingCostsThisSourceAndNothingElse(t *testing.T) {
	out := ReadRuns(unreachable(), "o/r", "2026-08-21T09:00:00Z", "2026-08-21T11:30:00Z")
	if len(out.Runs) != 0 || out.Watermark != "2026-08-21T09:00:00Z" || !strings.Contains(out.Error, "could not be read") {
		t.Fatalf("%+v", out)
	}
}

func TestTheFirstFoldLooksBackOnlyAsFarAsTheHourTierIsWide(t *testing.T) {
	if got := LookbackFrom("2026-08-21T11:00:00Z", FirstFoldLookbackDays); got != "2026-08-18T11:00:00.000Z" {
		t.Fatal(got)
	}
	r := routes(t, map[string]string{workitem.SchedulerWorkflowFile: runsJSON(), workitem.ExecutorWorkflowFile: runsJSON()})
	ReadRuns(r, "o/r", nil, "2026-08-21T11:00:00Z")
	if r.count(`2026-08-18`) != len(r.paths) || len(r.paths) != 2 {
		t.Fatalf("%v", r.paths)
	}
}

// --- the run costs ------------------------------------------------------------

const costNow = "2026-09-15T12:00:00Z"

func jobJSON(id int, from, to, conclusion string) string {
	end := "null"
	if to != "" {
		end = `"` + to + `"`
	}
	return `{"id": ` + strconv.Itoa(id) + `, "started_at": "` + from + `", "completed_at": ` + end + `, "conclusion": "` + conclusion + `", "name": "job-` + strconv.Itoa(id) + `"}`
}

// costAPI answers the two run listings, the jobs listings and the job
// logs the way GitHub does, 404 for anything it does not hold.
func costAPI(t *testing.T, scheduler, executor []string, jobs map[string][]string, logs map[string]string) *fakeReader {
	f := &fakeReader{logs: logs}
	f.answer = func(path string) (any, error) {
		switch {
		case strings.Contains(path, "claudinite-scheduler.yml/runs"):
			return js(t, runsJSON(scheduler...)), nil
		case strings.Contains(path, "claudinite-executor.yml/runs"):
			return js(t, runsJSON(executor...)), nil
		}
		if m := regexp.MustCompile(`/actions/runs/(\d+)/jobs`).FindStringSubmatch(path); m != nil {
			if found, ok := jobs[m[1]]; ok {
				return js(t, `{"jobs": [`+strings.Join(found, ",")+`]}`), nil
			}
		}
		return nil, nil
	}
	return f
}

func jobsOf(t *testing.T, jobs ...string) []any {
	return js(t, "["+strings.Join(jobs, ",")+"]").([]any)
}

func TestAJobIsBilledItsWallTimeRoundedUpToAWholeMinuteSummedOverTheRun(t *testing.T) {
	got := BilledMinutes(jobsOf(t,
		jobJSON(1, "2026-09-15T10:00:00Z", "2026-09-15T10:00:30Z", "success"),
		jobJSON(2, "2026-09-15T10:00:00Z", "2026-09-15T10:02:00Z", "success"),
		jobJSON(3, "2026-09-15T10:00:00Z", "2026-09-15T10:02:01Z", "success")))
	if got == nil || *got != 6 {
		t.Fatal(got)
	}
}

func TestAJobWithNoCompletionContributesNothingAndARunOfOnlyThoseIsUnknown(t *testing.T) {
	if got := BilledMinutes(jobsOf(t, jobJSON(1, "2026-09-15T10:00:00Z", "", "success"))); got != nil {
		t.Fatal(*got)
	}
	if got := BilledMinutes(nil); got != nil {
		t.Fatal(*got)
	}
}

func TestADaysReadsStayUnderTenCallsAtTheQueuesOwnCadence(t *testing.T) {
	api := costAPI(t,
		[]string{runJSON(1, "2026-09-15T05:10:00Z", "success"), runJSON(2, "2026-09-15T17:10:00Z", "success")},
		[]string{runJSON(3, "2026-09-15T05:12:00Z", "success"), runJSON(4, "2026-09-15T06:00:00Z", "success"), runJSON(5, "2026-09-15T17:12:00Z", "success")},
		map[string][]string{
			"1": {jobJSON(11, "2026-09-15T05:10:00Z", "2026-09-15T05:10:40Z", "success")},
			"2": {jobJSON(12, "2026-09-15T17:10:00Z", "2026-09-15T17:10:40Z", "success")},
			"3": {jobJSON(13, "2026-09-15T05:12:00Z", "2026-09-15T05:14:00Z", "success")},
			"4": {jobJSON(14, "2026-09-15T06:00:00Z", "2026-09-15T06:01:00Z", "success")},
			"5": {jobJSON(15, "2026-09-15T17:12:00Z", "2026-09-15T17:13:00Z", "success")},
		},
		map[string]string{
			"11": queue.RunCostTag + " v1 scheduler [1] calls=6 list=100 ask=200 drain=50",
			"12": queue.RunCostTag + " v1 scheduler [2] calls=6 list=100 ask=200 drain=50",
		})
	read, err := ReadRunCosts(api, "o/r", "2026-09-14T00:00:00Z", costNow, MaxRunReads)
	if err != nil || len(read.Runs) != 5 {
		t.Fatalf("%+v %v", read, err)
	}
	if len(api.paths) >= 10 {
		t.Fatalf("expected under ten calls, made %d: %v", len(api.paths), api.paths)
	}
}

func TestTheLogReadIsBoundedAtTwoTicksHoweverManySchedulerRunsAreWaiting(t *testing.T) {
	var ticks []string
	jobs, logs := map[string][]string{}, map[string]string{}
	for n := 1; n <= 4; n++ {
		id := strconv.Itoa(n)
		ticks = append(ticks, runJSON(n, "2026-09-15T0"+id+":10:00Z", "success"))
		jobs[id] = []string{jobJSON(n*10, "2026-09-15T05:10:00Z", "2026-09-15T05:11:00Z", "success")}
		logs[strconv.Itoa(n*10)] = queue.RunCostTag + " v1 scheduler [" + id + "] calls=6"
	}
	api := costAPI(t, ticks, nil, jobs, logs)
	if _, err := ReadRunCosts(api, "o/r", "2026-09-14T00:00:00Z", costNow, MaxRunReads); err != nil {
		t.Fatal(err)
	}
	if n := api.count(`/logs$`); n != SchedulerRunsPerFold {
		t.Fatalf("%d log reads", n)
	}
}

func TestAnExecutorRunReadsNoLogAtAll(t *testing.T) {
	api := costAPI(t, nil, []string{runJSON(9, "2026-09-15T05:12:00Z", "success")},
		map[string][]string{"9": {jobJSON(90, "2026-09-15T05:12:00Z", "2026-09-15T05:13:00Z", "success")}}, nil)
	if _, err := ReadRunCosts(api, "o/r", "2026-09-14T00:00:00Z", costNow, MaxRunReads); err != nil || api.count(`/logs$`) != 0 {
		t.Fatalf("%v %v", err, api.paths)
	}
}

func TestARunWhoseJobsListing404sKeepsItsRowAndLosesOnlyItsMinutes(t *testing.T) {
	api := costAPI(t, nil, []string{runJSON(9, "2026-09-15T05:12:00Z", "success")}, nil, nil)
	read, err := ReadRunCosts(api, "o/r", "2026-09-14T00:00:00Z", costNow, MaxRunReads)
	if err != nil || len(read.Runs) != 1 || read.Runs[0].Jobs != nil || read.Runs[0].MinutesBilled != nil {
		t.Fatalf("%+v %v", read, err)
	}
}

func TestTheCapStopsTheReadAndLeavesTheWatermarkAtTheLastRunMeasured(t *testing.T) {
	var runs []string
	jobs := map[string][]string{}
	for n := 1; n <= 5; n++ {
		runs = append(runs, runJSON(n, "2026-09-15T0"+strconv.Itoa(n)+":00:00Z", "success"))
		jobs[strconv.Itoa(n)] = []string{jobJSON(n*10, "2026-09-15T05:00:00Z", "2026-09-15T05:01:00Z", "success")}
	}
	read, err := ReadRunCosts(costAPI(t, nil, runs, jobs, nil), "o/r", "2026-09-14T00:00:00Z", costNow, 2)
	if err != nil || !read.Truncated || len(read.Runs) != 2 || read.Watermark != "2026-09-15T02:00:00Z" {
		t.Fatalf("%+v %v", read, err)
	}
}

func TestATickWhoseLogCarriesNoRecordIsATickWithNoCost(t *testing.T) {
	api := costAPI(t, []string{runJSON(1, "2026-09-15T05:10:00Z", "success")}, nil,
		map[string][]string{"1": {jobJSON(11, "2026-09-15T05:10:00Z", "2026-09-15T05:11:00Z", "success")}},
		map[string]string{"11": "nothing of interest here"})
	read, err := ReadRunCosts(api, "o/r", "2026-09-14T00:00:00Z", costNow, MaxRunReads)
	if err != nil || read.Runs[0].Cost != nil || read.Runs[0].MinutesBilled == nil || *read.Runs[0].MinutesBilled != 1 {
		t.Fatalf("%+v %v", read, err)
	}
}

func TestASkippedJobIsNeverOpened(t *testing.T) {
	api := costAPI(t, nil, nil, nil, map[string]string{"22": queue.RunCostTag + " v1 scheduler [1] calls=1"})
	record, reads, err := ReadCostFromLog(api, "o/r", jobsOf(t,
		jobJSON(21, "2026-09-15T05:10:00Z", "", "skipped"),
		jobJSON(22, "2026-09-15T05:10:00Z", "2026-09-15T05:11:00Z", "success")), JobLogsPerRun)
	if err != nil || reads != 1 || record == nil || record.RunID != "1" {
		t.Fatalf("%v %d %v", record, reads, err)
	}
}

// --- the closed items, off one timeline read ----------------------------------

const itemJSON = `{"number": 42, "title": "[claudinite-work] p/a", "body": "packs/p/tasks/a/task.md\n",
	"labels": [{"name": "task:status:done"}], "created_at": "2026-09-15T05:12:00Z", "closed_at": "2026-09-15T05:40:00Z"}`

func labeled(name, at string) string {
	return `{"event": "labeled", "label": {"name": "` + name + `"}, "created_at": "` + at + `"}`
}

func timelineJSON(events ...string) string { return "[" + strings.Join(events, ",") + "]" }

var timeline = []string{
	labeled("task:status:waiting-for-executor", "2026-09-15T05:12:00Z"),
	labeled("task:status:running-executor", "2026-09-15T05:20:00Z"),
	labeled("task:status:running-agent", "2026-09-15T05:25:00Z"),
	`{"event": "commented", "created_at": "2026-09-15T05:40:00Z", "body": "ran\n\n` + "```" + `\nclaudinite-task-exec v1 p/a [#42] success\n` +
		queue.RunCostTag + ` v1 executor [77] calls=31 pick=1000 claim=2000\n` + "```" + `"}`,
}

func TestTheFourLatenciesAreMeasuredBetweenTheEventsThatBoundEach(t *testing.T) {
	row := LatencyOf(js(t, itemJSON), js(t, timelineJSON(timeline...)).([]any), []string{"2026-09-15T05:10:00Z", "2026-09-15T17:10:00Z"})
	same(t, row, `{"tickToItemMinutes": 2, "itemToPickMinutes": 8, "pickToHandOffMinutes": 5, "handOffToConvergeMinutes": 15}`)
}

func TestAStepThatNeverHappenedHasNoKeyAtAll(t *testing.T) {
	agentless := []string{timeline[0], timeline[1], timeline[3]}
	row := LatencyOf(js(t, itemJSON), js(t, timelineJSON(agentless...)).([]any), nil)
	if row.Has("handOffToConvergeMinutes") || row.Has("tickToItemMinutes") || field(row, "itemToPickMinutes") != 8.0 {
		t.Fatalf("%s", Stringify(row))
	}
}

func TestTheTickAnItemIsMeasuredFromIsTheNewestOneThatHadAlreadyStarted(t *testing.T) {
	row := LatencyOf(js(t, itemJSON), js(t, timelineJSON(timeline...)).([]any),
		[]string{"2026-09-14T17:10:00Z", "2026-09-15T05:10:00Z", "2026-09-15T05:30:00Z"})
	if field(row, "tickToItemMinutes") != 2.0 {
		t.Fatalf("%s", Stringify(row))
	}
}

func TestAnItemPickedTwiceIsMeasuredFromTheFirstPick(t *testing.T) {
	events := append(append([]string{}, timeline...), labeled("task:status:running-executor", "2026-09-15T09:00:00Z"))
	row := LatencyOf(js(t, itemJSON), js(t, timelineJSON(events...)).([]any), nil)
	if field(row, "itemToPickMinutes") != 8.0 {
		t.Fatalf("%s", Stringify(row))
	}
}

func TestAParkKindIsCountedOnceForAnItemHoweverOftenItWasWorn(t *testing.T) {
	kinds := ParksIn(js(t, timelineJSON(
		labeled("task:status:needs-human-failure", "2026-09-15T05:30:00Z"),
		labeled("task:status:needs-human-failure", "2026-09-15T06:30:00Z"),
		labeled("task:status:needs-human-approval", "2026-09-15T07:30:00Z"),
		labeled("task:status:running-agent", "2026-09-15T08:30:00Z"))).([]any))
	if !reflect.DeepEqual(kinds, []string{"failure", "approval"}) {
		t.Fatalf("%v", kinds)
	}
}

func TestTheCostRecordsAnItemCarriesComeBackWithTheRunThatLeftThem(t *testing.T) {
	costs := CostsIn(js(t, timelineJSON(timeline...)).([]any))
	if len(costs) != 1 || costs[0].RunID != "77" || costs[0].APICalls == nil || *costs[0].APICalls != 31 {
		t.Fatalf("%+v", costs)
	}
}

// itemsAPI answers the closed-issue listing's first page with issues and
// each item's first timeline page from timelines.
func itemsAPI(t *testing.T, issues string, timelines map[string]string) *fakeReader {
	page := regexp.MustCompile(`[?&]page=(\d+)`)
	first := func(path string) bool { m := page.FindStringSubmatch(path); return m == nil || m[1] == "1" }
	return &fakeReader{answer: func(path string) (any, error) {
		if regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues\?`).MatchString(path) {
			if first(path) {
				return js(t, issues), nil
			}
			return js(t, "[]"), nil
		}
		if m := regexp.MustCompile(`/issues/(\d+)/timeline`).FindStringSubmatch(path); m != nil {
			if !first(path) {
				return js(t, "[]"), nil
			}
			if body, ok := timelines[m[1]]; ok {
				return js(t, body), nil
			}
		}
		return nil, nil
	}}
}

func TestOneItemCostsOneTimelineReadAndAnswersAllThreeQuestionsFromIt(t *testing.T) {
	api := itemsAPI(t, "["+itemJSON+"]", map[string]string{"42": timelineJSON(timeline...)})
	read := ReadClosedItems(api, "o/r", "2026-09-15T00:00:00Z", costNow, []string{"2026-09-15T05:10:00Z"})
	if api.count(`/timeline`) != 1 || len(read.Records) != 1 {
		t.Fatalf("%v %+v", api.paths, read)
	}
	rec := read.Records[0]
	if rec.Outcome != "done" || rec.Parks == nil || len(rec.Parks) != 0 || field(rec.Latency, "itemToPickMinutes") != 8.0 ||
		rec.Costs[0].RunID != "77" || read.Watermark != "2026-09-15T05:40:00Z" {
		t.Fatalf("%+v", read)
	}
}

func TestAnItemThatClosedAtOrBeforeTheMarkIsNotCountedASecondTime(t *testing.T) {
	api := itemsAPI(t, "["+itemJSON+"]", map[string]string{"42": timelineJSON(timeline...)})
	read := ReadClosedItems(api, "o/r", "2026-09-15T05:40:00Z", costNow, nil)
	if len(read.Records) != 0 || api.count(`/timeline`) != 0 {
		t.Fatalf("%+v %v", read, api.paths)
	}
}

func TestAnUnreadableTimelineCostsTheItemItsParksAndLatencyNeverItsOutcome(t *testing.T) {
	read := ReadClosedItems(itemsAPI(t, "["+itemJSON+"]", nil), "o/r", "2026-09-15T00:00:00Z", costNow, nil)
	if read.Records[0].Outcome != "done" || read.Records[0].Parks != nil || read.Records[0].Latency != nil {
		t.Fatalf("%+v", read.Records[0])
	}
}

func TestAPullRequestInTheIssuesListingIsNotAWorkItem(t *testing.T) {
	pr := strings.Replace(itemJSON, `"number": 42,`, `"number": 42, "pull_request": {},`, 1)
	if read := ReadClosedItems(itemsAPI(t, "["+pr+"]", nil), "o/r", "2026-09-15T00:00:00Z", costNow, nil); len(read.Records) != 0 {
		t.Fatalf("%+v", read)
	}
}

// --- the queue outcomes and parks ----------------------------------------------

func renderItemBody(taskPath string) string {
	return "someone's own words\n\n" + workitem.MachineBlockStart + "\n" + taskPath + "\n" + workitem.MachineBlockEnd + "\n"
}

// queueItem is an item as the issues API answers it, titled through the
// queue's own grammar, with over's keys laid on top.
func queueItem(t *testing.T, over string) *Obj {
	base := jsObj(t, `{"number": 1, "body": "", "labels": [], "state": "closed", "closed_at": "2026-08-20T10:00:00Z"}`)
	base.Set("title", workitem.WorkPrefix+" acme-pack-b/usage-fold")
	return spread(base, jsObj(t, over))
}

func labelled(name string) string { return `{"labels": [{"name": "` + name + `"}]}` }

func pages(t *testing.T, list ...[]*Obj) *fakeReader {
	page := regexp.MustCompile(`[?&]page=(\d+)`)
	return &fakeReader{answer: func(path string) (any, error) {
		n := 1
		if m := page.FindStringSubmatch(path); m != nil {
			n, _ = strconv.Atoi(m[1])
		}
		if n-1 >= len(list) {
			return []any{}, nil
		}
		out := []any{}
		for _, o := range list[n-1] {
			out = append(out, o)
		}
		return out, nil
	}}
}

func TestAnItemIsFiledUnderTheTaskItsTitleNamesAndItsOutcomeWord(t *testing.T) {
	rec, ok := QueueRecordFor(queueItem(t, labelled(workitem.OutcomeDone)), nil)
	want := QueueRecord{Date: "2026-08-20", ClosedAt: "2026-08-20T10:00:00Z", Pack: "acme-pack-b", Task: "usage-fold", Outcome: "done", Number: 1.0}
	if !ok || !reflect.DeepEqual(rec, want) {
		t.Fatalf("%+v", rec)
	}
	for label, word := range map[string]string{workitem.OutcomeDelivered: "delivered", workitem.OutcomeObsolete: "obsolete"} {
		if rec, _ := QueueRecordFor(queueItem(t, labelled(label)), nil); rec.Outcome != word {
			t.Errorf("%s: %s", label, rec.Outcome)
		}
	}
	if rec, _ := QueueRecordFor(queueItem(t, `{}`), nil); rec.Outcome != "none" {
		t.Errorf("an item closed wearing no outcome: %s", rec.Outcome)
	}
}

func TestAnAdoptedMarkedIssueIsFoundByItsMachineBlock(t *testing.T) {
	adopted := queueItem(t, `{"title": "the growth extract is picking up merge commits",
		"labels": [{"name": "`+workitem.OriginAdHoc+`"}, {"name": "`+workitem.OutcomeDone+`"}]}`)
	adopted.Set("body", renderItemBody("packs/acme-pack-b/tasks/acme-task/task.json"))
	rec, ok := QueueRecordFor(adopted, nil)
	if !ok || rec.Pack != "acme-pack-b" || rec.Task != "acme-task" {
		t.Fatalf("%+v", rec)
	}
}

func TestAnIssueThatIsNotAWorkItemOrHasNotClosedIsNotThisFoldsBusiness(t *testing.T) {
	if _, ok := QueueRecordFor(queueItem(t, `{"title": "a plain issue somebody filed", "body": "", "labels": []}`), nil); ok {
		t.Error("a plain issue")
	}
	if _, ok := QueueRecordFor(queueItem(t, `{"state": "open", "closed_at": null}`), nil); ok {
		t.Error("an open item")
	}
}

func TestOnlyItemsThatClosedPastTheMarkAreCounted(t *testing.T) {
	mark := "2026-08-20T10:00:00Z"
	if _, ok := QueueRecordFor(queueItem(t, `{"closed_at": "2026-08-20T10:00:00Z"}`), mark); ok {
		t.Error("exactly the mark — already counted")
	}
	if _, ok := QueueRecordFor(queueItem(t, `{"closed_at": "2026-08-20T09:00:00Z"}`), mark); ok {
		t.Error("closed before it")
	}
	if _, ok := QueueRecordFor(queueItem(t, `{"closed_at": "2026-08-20T11:00:00Z"}`), mark); !ok {
		t.Error("closed past it")
	}
}

func TestReadQueueOutcomesPagesFiltersPRsOutAndAdvancesTheMark(t *testing.T) {
	r := pages(t, []*Obj{
		queueItem(t, `{"number": 2, "closed_at": "2026-08-20T12:00:00Z", "labels": [{"name": "`+workitem.OutcomeDone+`"}]}`),
		queueItem(t, `{"number": 3, "pull_request": {"url": "x"}}`),
		queueItem(t, `{"number": 4, "closed_at": "2026-08-20T11:00:00Z"}`),
	})
	out := ReadQueueOutcomes(r, "o/r", "2026-08-20T10:00:00Z", "2026-08-21T11:00:00Z")
	var got [][2]string
	for _, rec := range out.Records {
		got = append(got, [2]string{rec.Task, rec.Outcome})
	}
	if !reflect.DeepEqual(got, [][2]string{{"usage-fold", "done"}, {"usage-fold", "none"}}) || out.Watermark != "2026-08-20T12:00:00Z" {
		t.Fatalf("%+v", out)
	}
	if r.count(`/issues\?`) != 1 || r.count(`/issues\?.*state=closed`) != 1 {
		t.Fatalf("a short page is the end of the window: %v", r.paths)
	}
}

func TestAFoldThatFoundNothingKeepsTheMarkItWasGiven(t *testing.T) {
	out := ReadQueueOutcomes(pages(t, nil), "o/r", "2026-08-20T10:00:00Z", "2026-08-21T11:00:00Z")
	if len(out.Records) != 0 || out.Watermark != "2026-08-20T10:00:00Z" {
		t.Fatalf("%+v", out)
	}
}

func TestAnUnreadableQueueListingCostsTheQueueRowsAndLeavesTheMarkAlone(t *testing.T) {
	out := ReadQueueOutcomes(unreachable(), "o/r", "2026-08-20T10:00:00Z", "2026-08-21T11:00:00Z")
	if len(out.Records) != 0 || out.Watermark != "2026-08-20T10:00:00Z" || !strings.Contains(out.Error, "could not be listed") {
		t.Fatalf("%+v", out)
	}
}

func TestTheFirstQueueReadCoversTheDayTiersWholeWidth(t *testing.T) {
	if got := LookbackFrom("2026-08-21T11:00:00Z", FirstReadLookbackDays)[:10]; got != "2026-07-22" {
		t.Fatal(got)
	}
	r := pages(t, nil)
	ReadQueueOutcomes(r, "o/r", nil, "2026-08-21T11:00:00Z")
	if !strings.Contains(r.paths[0], encodeURIComponent("2026-07-22")) {
		t.Fatal(r.paths[0])
	}
}

func TestParkKindOfDecodesEverySpellingAParkHasEverWorn(t *testing.T) {
	for label, want := range map[any]string{
		workitem.StatusNeedsHumanApprove:      "approval",
		workitem.StatusNeedsHumanFailure:      "failure",
		"task:needs-human-decision":           "decision",
		workitem.NeedsHuman:                   "failure",
		workitem.ParkPrefix + "something-new": "failure",
		workitem.OutcomeDone:                  "",
		workitem.OriginAdHoc:                  "",
		nil:                                   "",
	} {
		if got := ParkKindOf(label); got != want {
			t.Errorf("%v: %q, want %q", label, got, want)
		}
	}
}

func eventsReader(t *testing.T, events string) *fakeReader {
	return &fakeReader{answer: func(string) (any, error) {
		if events == "" {
			return nil, nil
		}
		return js(t, events), nil
	}}
}

func TestReadParksReadsTheLabelEvents(t *testing.T) {
	events := `[{"event": "labeled", "label": {"name": "` + workitem.StatusNeedsHumanApprove + `"}},
		{"event": "unlabeled", "label": {"name": "` + workitem.StatusNeedsHumanApprove + `"}},
		{"event": "labeled", "label": {"name": "` + workitem.StatusNeedsHumanApprove + `"}},
		{"event": "labeled", "label": {"name": "` + workitem.StatusNeedsHumanFailure + `"}},
		{"event": "labeled", "label": {"name": "` + workitem.OutcomeDone + `"}},
		{"event": "closed"}]`
	if got := ReadParks(eventsReader(t, events), "o/r", 5.0); !reflect.DeepEqual(got, []string{"approval", "failure"}) {
		t.Fatalf("%v", got)
	}
}

func TestReadParksAnswersNilNeverAnEmptyListWhenTheListingCannotBeRead(t *testing.T) {
	if got := ReadParks(eventsReader(t, ""), "o/r", 5.0); got != nil {
		t.Fatalf("%v", got)
	}
	if got := ReadParks(unreachable(), "o/r", 5.0); got != nil {
		t.Fatalf("%v", got)
	}
	if got := ReadParks(eventsReader(t, `[{"event": "closed"}]`), "o/r", 5.0); got == nil || len(got) != 0 {
		t.Fatalf("an item that was never parked is an empty list: %v", got)
	}
}

func TestReadQueueOutcomesReadsOneEventListingPerNewlyClosedItem(t *testing.T) {
	r := pages(t, []*Obj{
		queueItem(t, `{"number": 2, "closed_at": "2026-08-20T12:00:00Z", "labels": [{"name": "`+workitem.OutcomeDone+`"}]}`),
		queueItem(t, `{"number": 4, "closed_at": "2026-08-19T09:00:00Z"}`),
	})
	out := ReadQueueOutcomes(r, "o/r", "2026-08-20T10:00:00Z", "2026-08-21T11:00:00Z")
	if len(out.Records) != 1 || out.Records[0].Number != 2.0 || r.count(`/events`) != 1 {
		t.Fatalf("%+v %v", out, r.paths)
	}
}

// --- the merged pull requests --------------------------------------------------

func prJSON(over string) *Obj {
	o, _ := ParseJSON(`{"number": 1583, "created_at": "2026-08-19T20:00:00Z", "merged_at": "2026-08-20T10:00:00Z", "body": "does the thing\n\nCloses #1500\n"}`)
	p, _ := ParseJSON(over)
	return spread(o.(*Obj), p.(*Obj))
}

func prsAPI(t *testing.T, list []*Obj, issues map[string]string) *fakeReader {
	listing := pages(t, list)
	f := &fakeReader{}
	f.answer = func(path string) (any, error) {
		if m := regexp.MustCompile(`/issues/(\d+)`).FindStringSubmatch(path); m != nil {
			if body, ok := issues[m[1]]; ok {
				return js(t, body), nil
			}
			return nil, nil
		}
		return listing.answer(path)
	}
	return f
}

func TestTheClosingIssueIsTheOneTheBodyNamesFirstOnItsOwnLine(t *testing.T) {
	for body, want := range map[any]float64{
		"work\n\nCloses #1500\nCloses #1501\n": 1500, "Fixes #7": 7, "  resolves #8  ": 8,
		"Refs #1500": 0, "this closes #1500 eventually": 0, "": 0, nil: 0,
	} {
		got := ClosesIssueIn(body)
		if (want == 0) != (got == nil) || (got != nil && *got != want) {
			t.Errorf("%q: %v", body, got)
		}
	}
}

func TestASpanWithAnUnknownEndIsNilAndSoIsOneWhoseEndsDisagree(t *testing.T) {
	if h := HoursBetween("2026-08-20T00:00:00Z", "2026-08-20T12:30:00Z"); h == nil || *h != 12.5 {
		t.Fatal(h)
	}
	for _, pair := range [][2]any{{nil, "2026-08-20T12:30:00Z"}, {"2026-08-20T12:30:00Z", nil}, {"2026-08-20T12:30:00Z", "2026-08-20T00:00:00Z"}} {
		if h := HoursBetween(pair[0], pair[1]); h != nil {
			t.Errorf("%v: %v", pair, *h)
		}
	}
}

func TestReadMergedPrsKeepsMergesPastTheMarkReadsEachClosingIssueOnceAndAdvances(t *testing.T) {
	api := prsAPI(t, []*Obj{
		prJSON(`{}`),
		prJSON(`{"number": 1584, "merged_at": "2026-08-20T12:00:00Z", "body": "no keyword here"}`),
		prJSON(`{"number": 1585, "merged_at": null}`),
		prJSON(`{"number": 1586, "merged_at": "2026-08-19T10:00:00Z"}`),
	}, map[string]string{"1500": `{"created_at": "2026-08-18T20:00:00Z"}`})
	out := ReadMergedPrs(api, "o/r", "2026-08-20T00:00:00Z", "2026-08-21T11:00:00Z")
	if len(out.Prs) != 2 || out.Prs[0].Number != 1583.0 || out.Prs[1].Number != 1584.0 {
		t.Fatalf("%+v", out)
	}
	if out.Prs[0].IssueCreatedAt != "2026-08-18T20:00:00Z" || out.Prs[1].ClosesIssue != nil || out.Watermark != "2026-08-20T12:00:00Z" {
		t.Fatalf("%+v", out)
	}
	if api.count(`/issues/`) != 1 {
		t.Fatalf("%v", api.paths)
	}
}

func TestAnUnreadablePRListingCostsThePRRowsAndLeavesTheMarkAlone(t *testing.T) {
	out := ReadMergedPrs(unreachable(), "o/r", "2026-08-20T00:00:00Z", "2026-08-21T11:00:00Z")
	if len(out.Prs) != 0 || out.Watermark != "2026-08-20T00:00:00Z" || !strings.Contains(out.Error, "could not be listed") {
		t.Fatalf("%+v", out)
	}
}

func TestTheFirstPRReadCoversTheDayTiersWidth(t *testing.T) {
	api := prsAPI(t, []*Obj{prJSON(`{"merged_at": "2026-01-01T00:00:00Z"}`), prJSON(`{"number": 9, "merged_at": "2026-08-20T10:00:00Z"}`)}, nil)
	out := ReadMergedPrs(api, "o/r", nil, "2026-08-21T11:00:00Z")
	if len(out.Prs) != 1 || out.Prs[0].Number != 9.0 {
		t.Fatalf("%+v", out)
	}
}

func TestPrRecordsFromJoinsTheListingToTheSessionThatDidTheWork(t *testing.T) {
	files := []CaptureFile{
		{Date: "2026-08-19", Stamp: "2026-08-19T18:00:00Z", Issue: fp(1500), SessionID: "s1"},
		{Date: "2026-08-19", Stamp: "2026-08-19T21:00:00Z", Issue: fp(1500), SessionID: "s1"},
		{Date: "2026-08-19", Stamp: "2026-08-19T09:00:00Z", Issue: fp(0), SessionID: "s2"},
	}
	recs := PrRecordsFrom([]MergedPr{{Number: 1583.0, MergedAt: "2026-08-20T10:00:00Z", CreatedAt: "2026-08-19T20:00:00Z",
		ClosesIssue: fp(1500), IssueCreatedAt: "2026-08-18T20:00:00Z"}}, files)
	want := PrRecord{Date: "2026-08-20", Number: 1583.0, LeadHours: fp(14), IssueLeadHours: fp(38), SessionToMerge: fp(16)}
	if !reflect.DeepEqual(recs, []PrRecord{want}) {
		t.Fatalf("%+v", recs[0])
	}
}

func TestAPRKeyedCaptureJoinsByPRNumber(t *testing.T) {
	recs := PrRecordsFrom([]MergedPr{{Number: 1600.0, MergedAt: "2026-08-20T10:00:00Z", CreatedAt: "2026-08-20T08:00:00Z"}}, []CaptureFile{
		{Date: "2026-08-20", Stamp: "2026-08-20T06:00:00Z", PR: fp(1600), SessionID: "s3"},
		{Date: "2026-08-20", Stamp: "2026-08-20T11:00:00Z", Issue: fp(0), SessionID: "s3"},
	})
	want := PrRecord{Date: "2026-08-20", Number: 1600.0, LeadHours: fp(2), SessionToMerge: fp(4)}
	if !reflect.DeepEqual(recs, []PrRecord{want}) {
		t.Fatalf("%+v", recs[0])
	}
}

func TestAPRWhoseIssueNeverCapturedHasNoSessionLeadTime(t *testing.T) {
	recs := PrRecordsFrom([]MergedPr{{Number: 9.0, MergedAt: "2026-08-20T10:00:00Z", ClosesIssue: fp(1500)}}, nil)
	if !reflect.DeepEqual(recs, []PrRecord{{Date: "2026-08-20", Number: 9.0}}) {
		t.Fatalf("%+v", recs[0])
	}
}
