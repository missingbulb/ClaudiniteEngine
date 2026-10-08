package usage

import (
	"math"
	"reflect"
	"testing"
)

const (
	tasksToday = "2026-09-15"
	tasksNow   = "2026-09-15T12:00:00Z"
)

func tasksRun(id float64, workflow, at string) TasksRun {
	return TasksRun{ID: id, Workflow: workflow, StartedAt: at, Conclusion: "success", Jobs: fp(1), MinutesBilled: fp(2)}
}

func execCost(runID string, calls float64, phases map[string]float64) RunCost {
	return RunCost{RunID: runID, APICalls: fp(calls), PhaseMs: phases}
}

func closedItem(number float64, date string) ClosedItem {
	return ClosedItem{Number: number, Date: date, ClosedAt: date + "T05:40:00Z", Pack: "p", Task: "a", Outcome: "done",
		Parks: []string{}, Latency: NewObj(), Costs: []RunCost{}}
}

func TestARunLandsInItsWorkflowRowAndInTheDayItStarted(t *testing.T) {
	days := FoldTasksDays(nil, []TasksRun{tasksRun(1, "scheduler", "2026-09-15T05:10:00Z")}, nil, tasksToday, nil)
	day := days.ObjAt(tasksToday)
	if field(day, "workflows.scheduler.runs") != 1.0 || field(day, "workflows.scheduler.minutesBilled") != 2.0 ||
		field(day, "runs") != 1.0 || field(day, "minutesBilled") != 2.0 {
		t.Fatalf("%s", Stringify(day))
	}
}

func TestARepoWithNoConfiguredRateCarriesNoSpendAnywhere(t *testing.T) {
	day := FoldTasksDays(nil, []TasksRun{tasksRun(1, "executor", "2026-09-15T05:10:00Z")}, nil, tasksToday, nil).ObjAt(tasksToday)
	if day.Has("spend") || day.ObjAt("workflows").ObjAt("executor").Has("spend") {
		t.Fatalf("%s", Stringify(day))
	}
}

func TestAConfiguredRatePricesTheMinutesThatWereMeasuredAndNothingElse(t *testing.T) {
	unmeasured := tasksRun(2, "executor", "2026-09-15T06:10:00Z")
	unmeasured.MinutesBilled = nil
	day := FoldTasksDays(nil, []TasksRun{tasksRun(1, "executor", "2026-09-15T05:10:00Z"), unmeasured}, nil, tasksToday, fp(0.008)).ObjAt(tasksToday)
	spend, _ := field(day, "spend").(float64)
	if field(day, "workflows.executor.runs") != 2.0 || field(day, "minutesBilled") != 2.0 || math.Abs(spend-0.016) >= 1e-9 {
		t.Fatalf("%s", Stringify(day))
	}
}

func TestARunWhoseJobsListingFailedStillCountsAsARun(t *testing.T) {
	r := tasksRun(1, "executor", "2026-09-15T05:10:00Z")
	r.Jobs, r.MinutesBilled = nil, nil
	day := FoldTasksDays(nil, []TasksRun{r}, nil, tasksToday, nil).ObjAt(tasksToday)
	if field(day, "runs") != 1.0 || day.Has("jobs") || day.Has("minutesBilled") {
		t.Fatalf("%s", Stringify(day))
	}
}

func TestARunsCostRecordLandsInTheBucketsScalarsAndInItsOwnPerRunRow(t *testing.T) {
	r := tasksRun(1, "scheduler", "2026-09-15T05:10:00Z")
	r.Cost = &RunCost{RunID: "1", APICalls: fp(6), PhaseMs: map[string]float64{"list": 100, "ask": 200}}
	day := FoldTasksDays(nil, []TasksRun{r}, nil, tasksToday, nil).ObjAt(tasksToday)
	if field(day, "apiCalls") != 6.0 || field(day, "list") != 100.0 {
		t.Fatalf("%s", Stringify(day))
	}
	same(t, day.ObjAt("runCosts").ObjAt("1"), `{"apiCalls": 6, "list": 100, "ask": 200}`)
}

func TestSeveralStampsOfOneExecutorRunCollapseToTheLargest(t *testing.T) {
	items := []ClosedItem{closedItem(1, tasksToday), closedItem(2, tasksToday), closedItem(3, tasksToday)}
	items[0].Costs = []RunCost{execCost("77", 10, map[string]float64{"pick": 100})}
	items[1].Costs = []RunCost{execCost("77", 25, map[string]float64{"pick": 250, "converge": 40})}
	items[2].Costs = []RunCost{execCost("77", 18, map[string]float64{"pick": 180})}
	byRun := CostsByRun(items)
	if len(byRun) != 1 || *byRun["77"].APICalls != 25 || !reflect.DeepEqual(byRun["77"].PhaseMs, map[string]float64{"pick": 250, "converge": 40}) {
		t.Fatalf("%+v", byRun)
	}
}

func TestAnExecutorRunsCostIsAttachedFromTheItemsItSettled(t *testing.T) {
	item := closedItem(1, tasksToday)
	item.Costs = []RunCost{execCost("77", 25, map[string]float64{"pick": 250})}
	runs := WithItemCosts([]TasksRun{tasksRun(77, "executor", "2026-09-15T05:10:00Z")}, []ClosedItem{item})
	if runs[0].Cost == nil || *runs[0].Cost.APICalls != 25 {
		t.Fatalf("%+v", runs[0])
	}
}

func TestACostRecordWhoseRunTheListingNeverSawInventsNoBucketForIt(t *testing.T) {
	item := closedItem(1, tasksToday)
	item.Costs = []RunCost{execCost("999", 25, map[string]float64{"pick": 250})}
	day := FoldTasksDays(nil, WithItemCosts(nil, []ClosedItem{item}), []ClosedItem{item}, tasksToday, nil).ObjAt(tasksToday)
	same(t, day.ObjAt("runCosts"), `{}`)
	if day.Has("apiCalls") {
		t.Fatalf("%s", Stringify(day))
	}
}

func TestAnItemLandsUnderItsTaskWithItsParksAndItsLatencySamples(t *testing.T) {
	item := closedItem(42, tasksToday)
	item.Parks, item.Latency = []string{"failure"}, ObjOf("itemToPickMinutes", 8.0)
	day := FoldTasksDays(nil, nil, []ClosedItem{item}, tasksToday, nil).ObjAt(tasksToday)
	same(t, day.ObjAt("queue").ObjAt("p/a"), `{"done": 1}`)
	same(t, day.ObjAt("parks").ObjAt("p/a"), `{"failure": 1}`)
	same(t, day.ObjAt("latency").ObjAt("42"), `{"itemToPickMinutes": 8}`)
}

func TestAnItemWhoseTimelineCouldNotBeReadKeepsItsOutcomeAndNoParkRow(t *testing.T) {
	item := closedItem(42, tasksToday)
	item.Parks, item.Latency = nil, nil
	day := FoldTasksDays(nil, nil, []ClosedItem{item}, tasksToday, nil).ObjAt(tasksToday)
	same(t, day.ObjAt("queue").ObjAt("p/a"), `{"done": 1}`)
	same(t, day.ObjAt("parks"), `{}`)
	same(t, day.ObjAt("latency"), `{}`)
}

func TestAnHourRowCarriesTheRunCostsAndNoneOfTheItemDerivedMaps(t *testing.T) {
	hour := FoldTasksHours(nil, []TasksRun{tasksRun(1, "scheduler", "2026-09-15T05:10:00Z")}, tasksNow, nil).ObjAt("2026-09-15T05")
	if field(hour, "workflows.scheduler.runs") != 1.0 || hour.Has("queue") {
		t.Fatalf("%s", Stringify(hour))
	}
}

func TestAWeekAbsorbsADayThatClosedAndSaysHowManyRowsItTook(t *testing.T) {
	item := closedItem(42, "2026-09-14")
	item.Latency = ObjOf("itemToPickMinutes", 8.0)
	day := FoldTasksDays(nil, []TasksRun{tasksRun(1, "executor", "2026-09-14T05:10:00Z")}, []ClosedItem{item}, tasksToday, nil).ObjAt("2026-09-14")
	week := AddTasksDayToWeek(nil, day)
	if field(week, "days") != 1.0 || field(week, "runs") != 1.0 || (week.Has("runCosts") && week.ObjAt("runCosts").Len() > 0) {
		t.Fatalf("%s", Stringify(week))
	}
	same(t, week.ObjAt("queue").ObjAt("p/a"), `{"done": 1}`)
	same(t, week.ObjAt("latency").ObjAt("42"), `{"itemToPickMinutes": 8}`)
}

func TestTheWeekTierKeepsNoPerRunMap(t *testing.T) {
	if has(WeekGroups, "runCosts") {
		t.Fatal(WeekGroups)
	}
}

func TestAWeekAbsorbsEachDayExactlyOnce(t *testing.T) {
	now := tasksNow
	runs := []TasksRun{tasksRun(1, "executor", "2026-09-14T05:10:00Z"), tasksRun(2, "executor", "2026-09-14T17:10:00Z")}
	first := FoldTasksUsage(TasksFoldIn{Today: tasksToday, Now: &now, Runs: runs, RunsFoldedThrough: "2026-09-14T17:10:00Z"})
	second := FoldTasksUsage(TasksFoldIn{Prior: first, Today: tasksToday, Now: &now})
	week := IsoWeek("2026-09-14")
	if field(first.Weeks.ObjAt(week), "days") != 1.0 || field(second.Weeks.ObjAt(week), "days") != 1.0 || field(second.Weeks.ObjAt(week), "runs") != 2.0 {
		t.Fatalf("%s %s", Stringify(first.Weeks), Stringify(second.Weeks))
	}
}

func TestAWeekFrozenBeforeACounterExistedGrowsItFromTheFirstDayThatHasIt(t *testing.T) {
	week := AddTasksDayToWeek(jsObj(t, `{"days": 3, "runs": 5}`), jsObj(t, `{"runs": 1, "apiCalls": 7, "workflows": {}, "queue": {}, "parks": {}, "latency": {}}`))
	if field(week, "days") != 4.0 || field(week, "runs") != 6.0 || field(week, "apiCalls") != 7.0 {
		t.Fatalf("%s", Stringify(week))
	}
}

func roundTripTasks(t *testing.T, f TasksUsageFile) TasksUsageFile {
	return DecodeTasksUsage(js(t, RenderTasksUsageFile(EncodeTasksUsage(f))))
}

func TestAFoldedTasksFileRoundTripsThroughTheOnDiskTupleShape(t *testing.T) {
	now := tasksNow
	r := tasksRun(1, "scheduler", "2026-09-15T05:10:00Z")
	r.Cost = &RunCost{RunID: "1", APICalls: fp(6), PhaseMs: map[string]float64{"list": 100}}
	item := closedItem(42, tasksToday)
	item.Parks, item.Latency = []string{"approval"}, ObjOf("itemToPickMinutes", 8.0)
	back := roundTripTasks(t, FoldTasksUsage(TasksFoldIn{Today: tasksToday, Now: &now, Generated: now, MinuteRate: fp(0.008),
		Runs: []TasksRun{r}, Items: []ClosedItem{item}}))
	day := back.Days.ObjAt(tasksToday)
	if back.MinuteRate != 0.008 || field(day, "apiCalls") != 6.0 {
		t.Fatalf("%+v", back)
	}
	same(t, day.ObjAt("runCosts").ObjAt("1"), `{"apiCalls": 6, "list": 100}`)
	same(t, day.ObjAt("parks").ObjAt("p/a"), `{"approval": 1}`)
	same(t, day.ObjAt("latency").ObjAt("42"), `{"itemToPickMinutes": 8}`)
}

func TestATasksRecomputeThatFoundNothingNewRendersByteIdenticallyApartFromItsStamp(t *testing.T) {
	now := tasksNow
	in := TasksFoldIn{Today: tasksToday, Now: &now, Runs: []TasksRun{tasksRun(1, "executor", "2026-09-15T05:10:00Z")}}
	in.Generated = "2026-09-15T12:00:00Z"
	a := RenderTasksUsageFile(EncodeTasksUsage(FoldTasksUsage(in)))
	in.Generated = "2026-09-15T18:00:00Z"
	b := RenderTasksUsageFile(EncodeTasksUsage(FoldTasksUsage(in)))
	if a == b || WithoutStamp(a) != WithoutStamp(b) {
		t.Fatalf("%q\n%q", a, b)
	}
}

func TestAnUnknownTasksCounterDecodesToNoKeyRatherThanToAZero(t *testing.T) {
	now := tasksNow
	r := tasksRun(1, "executor", "2026-09-15T05:10:00Z")
	r.Jobs, r.MinutesBilled = nil, nil
	back := roundTripTasks(t, FoldTasksUsage(TasksFoldIn{Today: tasksToday, Now: &now, Runs: []TasksRun{r}}))
	day := back.Days.ObjAt(tasksToday)
	if field(day, "runs") != 1.0 || day.Has("minutesBilled") {
		t.Fatalf("%s", Stringify(day))
	}
}
