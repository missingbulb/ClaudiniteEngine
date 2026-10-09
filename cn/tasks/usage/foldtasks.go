package usage

import (
	"math"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
)

// The machinery half's folding core. Every tier is append-once: each
// source is a listing read past its own watermark, so a run or an item is
// folded on the one pass that first sees it. Unknown is not zero: a field
// starts at the first bucket that has an opinion about it.

// RunCost is one claudinite-run-cost record: the API calls, nil when
// unread, and each phase's milliseconds.
type RunCost struct {
	RunID    string
	APICalls *float64
	PhaseMs  map[string]float64
}

// TasksRun is one run the machinery half read: its jobs count and billed
// minutes, nil where the jobs listing could not be read, and its cost
// record, nil where none was found.
type TasksRun struct {
	ID            any
	StartedAt     string
	Workflow      string
	Conclusion    any
	Jobs          *float64
	MinutesBilled *float64
	Cost          *RunCost
}

// ClosedItem is one closed work item the machinery half read; its parks,
// latency and costs are nil where its timeline could not be read.
type ClosedItem struct {
	Date, ClosedAt, Pack, Task, Outcome string
	Number, CreatedAt                   any
	Parks                               []string
	Latency                             *Obj
	Costs                               []RunCost
}

// addFinite is row[field] = (row[field] ?? 0) + n for a finite n.
func addFinite(row *Obj, field string, n *float64) {
	if n == nil || math.IsNaN(*n) || math.IsInf(*n, 0) {
		return
	}
	bumpOr(row, field, *n)
}

func f64(v float64) *float64 { return &v }

func emptyBucket(groups []string) *Obj {
	o := NewObj()
	for _, g := range groups {
		o.Set(g, NewObj())
	}
	return o
}

// AddRunToBucket is one run's contribution to a bucket: its workflow's
// row, the bucket's own scalars and, where the tier keeps them, its
// per-run entry.
func AddRunToBucket(bucket *Obj, run TasksRun, minuteRate *float64, withRunCosts bool) {
	workflow := bucket.Sub("workflows").Sub(run.Workflow)
	var spend *float64
	if minuteRate != nil && run.MinutesBilled != nil && isFiniteNumber(*run.MinutesBilled) {
		spend = f64(*run.MinutesBilled * *minuteRate)
	}
	for i, n := range []*float64{f64(1), run.Jobs, run.MinutesBilled, spend} {
		addFinite(workflow, WorkflowFields[i], n)
		addFinite(bucket, WorkflowFields[i], n)
	}
	if run.Cost == nil {
		return
	}
	phases := func(into *Obj) {
		for _, p := range RunPhases {
			if ms, ok := run.Cost.PhaseMs[p]; ok {
				addFinite(into, p, f64(ms))
			}
		}
	}
	addFinite(bucket, "apiCalls", run.Cost.APICalls)
	phases(bucket)
	if withRunCosts {
		entry := bucket.Sub("runCosts").Sub(jsString(run.ID))
		addFinite(entry, "apiCalls", run.Cost.APICalls)
		phases(entry)
	}
}

// CostsByRun are the cost records the items carry, per run, reduced by
// maximum: one run leaves a snapshot on every item it settled.
func CostsByRun(items []ClosedItem) map[string]RunCost {
	out := map[string]RunCost{}
	for _, item := range items {
		for _, cost := range item.Costs {
			prior, ok := out[cost.RunID]
			if !ok {
				out[cost.RunID] = cost
				continue
			}
			calls := math.Max(orNegInf(prior.APICalls), orNegInf(cost.APICalls))
			phases := map[string]float64{}
			for p := range prior.PhaseMs {
				phases[p] = 0
			}
			for p := range cost.PhaseMs {
				phases[p] = 0
			}
			for p := range phases {
				phases[p] = math.Max(prior.PhaseMs[p], cost.PhaseMs[p])
			}
			out[cost.RunID] = RunCost{RunID: cost.RunID, APICalls: &calls, PhaseMs: phases}
		}
	}
	for id, c := range out {
		if c.APICalls != nil && (math.IsInf(*c.APICalls, 0) || math.IsNaN(*c.APICalls)) {
			c.APICalls = nil
			out[id] = c
		}
	}
	return out
}

func orNegInf(p *float64) float64 {
	if p == nil {
		return math.Inf(-1)
	}
	return *p
}

// WithItemCosts are the runs with each executor run's cost record
// attached from the items it settled.
func WithItemCosts(runs []TasksRun, items []ClosedItem) []TasksRun {
	byRun := CostsByRun(items)
	out := make([]TasksRun, len(runs))
	for i, r := range runs {
		if r.Cost == nil {
			if c, ok := byRun[jsString(r.ID)]; ok {
				c := c
				r.Cost = &c
			}
		}
		out[i] = r
	}
	return out
}

// AddItemToDay is one closed item's outcome, parks and latency sample.
func AddItemToDay(day *Obj, item ClosedItem) {
	key := item.Pack + "/" + item.Task
	outcome := item.Outcome
	if !has(QueueOutcomes, outcome) {
		outcome = "none"
	}
	addFinite(day.Sub("queue").Sub(key), outcome, f64(1))
	for _, kind := range item.Parks {
		if has(workitem.ParkKinds, kind) {
			addFinite(day.Sub("parks").Sub(key), kind, f64(1))
		}
	}
	if item.Latency.Len() > 0 {
		day.Sub("latency").Set(jsString(item.Number), spread(item.Latency))
	}
}

// WithinDayWindow reports a date inside the machinery file's day window.
func WithinDayWindow(date, today string) bool { return WithinTaskWindow(date, today, DayWindowDays) }

// FoldTasksHours is the machinery hour rows.
func FoldTasksHours(prior *Obj, runs []TasksRun, now string, minuteRate *float64) *Obj {
	hours := NewObj()
	for _, key := range prior.Keys() {
		if !WithinHourWindow(key, now, HourWindowHours) {
			continue
		}
		row, _ := prior.Get(key)
		hours.Set(key, spread(emptyBucket(HourGroups), spreadOf(clone(row))))
	}
	for _, run := range runs {
		key := HourKey(run.StartedAt)
		if !WithinHourWindow(key, now, HourWindowHours) {
			continue
		}
		b := hours.ObjAt(key)
		if b == nil {
			b = emptyBucket(HourGroups)
			hours.Set(key, b)
		}
		AddRunToBucket(b, run, minuteRate, true)
	}
	return hours
}

// FoldTasksDays is the machinery day rows.
func FoldTasksDays(prior *Obj, runs []TasksRun, items []ClosedItem, today string, minuteRate *float64) *Obj {
	days := NewObj()
	for _, date := range prior.Keys() {
		if !WithinDayWindow(date, today) {
			continue
		}
		row, _ := prior.Get(date)
		days.Set(date, spread(emptyBucket(TasksCounterGroups), spreadOf(clone(row))))
	}
	bucket := func(date string) *Obj {
		b := days.ObjAt(date)
		if b == nil {
			b = emptyBucket(TasksCounterGroups)
			days.Set(date, b)
		}
		return b
	}
	for _, run := range runs {
		date := sliceUnits(run.StartedAt, 0, 10)
		if date == "" || !WithinDayWindow(date, today) {
			continue
		}
		AddRunToBucket(bucket(date), run, minuteRate, true)
	}
	for _, item := range items {
		if item.Date == "" || !WithinDayWindow(item.Date, today) {
			continue
		}
		AddItemToDay(bucket(item.Date), item)
	}
	return days
}

// AddTasksDayToWeek adds one closed day into its week, the latency
// samples as they stand and every other group summed.
func AddTasksDayToWeek(week *Obj, day *Obj) *Obj {
	w := week
	if w == nil {
		w = emptyBucket(WeekGroups)
	}
	for _, g := range WeekGroups {
		w.Sub(g)
	}
	addFinite(w, "days", f64(1))
	addRow := func(into *Obj, from any, fields []string) {
		for _, f := range fields {
			if v, _ := propOf(from, f); isFiniteNumber(v) {
				addFinite(into, f, f64(v.(float64)))
			}
		}
	}
	addRow(w, day, BucketFields)
	for _, g := range WeekGroups {
		v, _ := day.Get(g)
		keys, rows := entriesOf(v)
		for i, key := range keys {
			if g == "latency" {
				w.ObjAt("latency").Set(key, spreadOf(rows[i]))
				continue
			}
			addRow(w.ObjAt(g).Sub(key), rows[i], TasksFields[g])
		}
	}
	return w
}

// TasksFoldIn is one machinery fold's inputs.
type TasksFoldIn struct {
	Prior              TasksUsageFile
	Today              string
	Now                *string
	Generated          any
	MinuteRate         *float64
	Runs               []TasksRun
	RunsFoldedThrough  any
	Items              []ClosedItem
	QueueFoldedThrough any
}

// FoldTasksUsage folds one run of the machinery half.
func FoldTasksUsage(in TasksFoldIn) TasksUsageFile {
	runs := WithItemCosts(in.Runs, in.Items)
	days := FoldTasksDays(orObj(in.Prior.Days), runs, in.Items, in.Today, in.MinuteRate)
	weeks := cloneObj(in.Prior.Weeks)
	foldedThrough := in.Prior.FoldedThrough
	for _, date := range DaysToFold(days, foldedThrough, in.Today) {
		key := IsoWeek(date)
		weeks.Set(key, AddTasksDayToWeek(weeks.ObjAt(key), days.ObjAt(date)))
		foldedThrough = date
	}
	now := in.Today + "T23:59:59Z"
	if in.Now != nil {
		now = *in.Now
	}
	var rate any
	if in.MinuteRate != nil {
		rate = *in.MinuteRate
	} else {
		rate = in.Prior.MinuteRate
	}
	return TasksUsageFile{
		Generated:          in.Generated,
		FoldedThrough:      foldedThrough,
		RunsFoldedThrough:  firstOf(in.RunsFoldedThrough, in.Prior.RunsFoldedThrough),
		QueueFoldedThrough: firstOf(in.QueueFoldedThrough, in.Prior.QueueFoldedThrough),
		MinuteRate:         rate,
		Hours:              sortKeys(FoldTasksHours(orObj(in.Prior.Hours), runs, now, in.MinuteRate)),
		Days:               sortKeys(days),
		Weeks:              sortKeys(weeks),
	}
}
