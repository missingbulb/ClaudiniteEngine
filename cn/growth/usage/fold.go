package usage

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// The session half's folding core: hour rows over the last three days, day
// rows recomputed from the live capture files with the appended sources
// carried in, week rows advanced once past the day watermark. Pure: the
// caller supplies today, the prior file and what each source answered.

// CaptureFile is one capture file still on the logs branch: what its name
// says and what its entries count to. Issue and PR are nil where the name
// carries the other key.
type CaptureFile struct {
	Date, Stamp string
	Issue, PR   *float64
	SessionID   string
	Counts      Counts
}

// keyed is the number the file's name carries, its PR's else its issue's.
func (f CaptureFile) keyed() any {
	if f.PR != nil {
		return *f.PR
	}
	if f.Issue != nil {
		return *f.Issue
	}
	return nil
}

func positive(p *float64) bool { return p != nil && *p > 0 }

// typeError is the TypeError strict-mode JavaScript raises writing a
// property onto a primitive, which a prior file's junk row raises.
func typeError(key string, v any) error {
	kind := "object"
	switch v.(type) {
	case float64:
		kind = "number"
	case string:
		kind = "string"
	case bool:
		kind = "boolean"
	case []any:
		return fmt.Errorf("a prior row holds an array where an object is folded into, at %q", key)
	}
	return fmt.Errorf("cannot create property '%s' on %s '%s'", key, kind, jsString(v))
}

func emptyDay() *Obj {
	o := zeros(CaptureDayFields)
	for _, m := range BareMaps {
		o.Set(m, NewObj())
	}
	for _, g := range CounterGroups {
		o.Set(g, NewObj())
	}
	return o
}

func emptyHour() *Obj {
	o := zeros(UsageFields["hour"])
	o.Set("taskExec", NewObj())
	return o
}

// subOf is (o[k] ??= {}), and the TypeError writing into a primitive
// there would raise.
func subOf(o *Obj, k string) (*Obj, any, error) {
	v, ok := o.Get(k)
	if !ok || v == nil {
		s := NewObj()
		o.Set(k, s)
		return s, s, nil
	}
	if s, isObj := v.(*Obj); isObj {
		return s, s, nil
	}
	return nil, v, nil
}

func addLoads(into any, from any) error {
	keys, vals := entriesOf(from)
	if len(keys) == 0 {
		return nil
	}
	o, ok := into.(*Obj)
	if !ok {
		return typeError(keys[0], into)
	}
	for i, k := range keys {
		bumpOr(o, k, vals[i])
	}
	return nil
}

// addCounters folds one map of counter rows into another key-wise; a
// field its group declares a peak takes the larger, every other adds.
func addCounters(into any, from any, group string) error {
	peaks := maxFields[group]
	target, isObj := into.(*Obj)
	keys, rows := entriesOf(from)
	for i, key := range keys {
		row := rows[i]
		var cur any
		held := false
		if isObj {
			cur, held = target.Get(key)
		}
		fields, ns := entriesOf(row)
		var t *Obj
		if !held || cur == nil {
			if row == nil {
				return errors.New("cannot convert undefined or null to object")
			}
			if !isObj {
				return typeError(key, into)
			}
			t = zeros(fields)
			target.Set(key, t)
		} else if t, held = cur.(*Obj); !held {
			if len(fields) > 0 {
				return typeError(fields[0], cur)
			}
			continue
		}
		for j, f := range fields {
			v, ok := t.Get(f)
			if has(peaks, f) {
				t.Set(f, jsMax(orZero(v, ok), ns[j]))
			} else {
				t.Set(f, plus(orZero(v, ok), ns[j]))
			}
		}
	}
	return nil
}

type sessionFacts struct {
	tokens        *Tokens
	tokensByModel *Obj
	seconds       *Seconds
	task          *string
	userMessages  float64
}

// orderedSet is a JavaScript Set of strings: its size and its order.
type orderedSet struct {
	order []string
	has   map[string]bool
}

func (s *orderedSet) add(v string) {
	if s.has == nil {
		s.has = map[string]bool{}
	}
	if !s.has[v] {
		s.has[v] = true
		s.order = append(s.order, v)
	}
}

// byName is a Map from a name to the sessions that attested it.
type byName struct {
	order []string
	sets  map[string]*orderedSet
}

func (b *byName) touch(name, session string) {
	if b.sets == nil {
		b.sets = map[string]*orderedSet{}
	}
	s := b.sets[name]
	if s == nil {
		s = &orderedSet{}
		b.sets[name] = s
		b.order = append(b.order, name)
	}
	s.add(session)
}

// FoldDays recomputes the day rows from the live capture files: a day is a
// pure function of the files stamped with it.
func FoldDays(files []CaptureFile) (*Obj, error) {
	days := NewObj()
	sessionsByDay := map[string]*orderedSet{}
	skillSessions := map[string]*byName{}
	ruleSessions := map[string]*byName{}
	var dateOrder []string
	perSession := map[string]*struct {
		order []string
		facts map[string]*sessionFacts
	}{}
	var sessionDates []string
	touch := func(into map[string]*byName, f CaptureFile, name string) {
		b := into[f.Date]
		if b == nil {
			b = &byName{}
			into[f.Date] = b
		}
		b.touch(name, f.SessionID)
	}
	for _, f := range files {
		day := days.ObjAt(f.Date)
		if day == nil {
			day = emptyDay()
			days.Set(f.Date, day)
		}
		bump(day, "captures", 1)
		if positive(f.PR) || positive(f.Issue) {
			bump(day, "merges", 1)
		}
		bump(day, "userMessages", f.Counts.UserMessages)
		bump(day, "userCommands", f.Counts.UserCommands)
		for _, m := range BareMaps {
			if m == "skillSessions" {
				continue
			}
			if err := addLoads(day.ObjAt(m), f.Counts.group(m)); err != nil {
				return nil, err
			}
		}
		for _, g := range []string{"checks", "checkFindings", "taskExec", "skillLoadsBy", "triggerFires", "guardFires", "checkTiming"} {
			if err := addCounters(day.ObjAt(g), f.Counts.group(g), g); err != nil {
				return nil, err
			}
		}
		for _, skill := range f.Counts.SkillLoadsBy.Keys() {
			touch(skillSessions, f, skill)
		}
		for _, rule := range f.Counts.CheckFindings.Keys() {
			touch(ruleSessions, f, rule)
		}
		if sessionsByDay[f.Date] == nil {
			sessionsByDay[f.Date] = &orderedSet{}
			dateOrder = append(dateOrder, f.Date)
		}
		sessionsByDay[f.Date].add(f.SessionID)

		ps := perSession[f.Date]
		if ps == nil {
			ps = &struct {
				order []string
				facts map[string]*sessionFacts
			}{facts: map[string]*sessionFacts{}}
			perSession[f.Date] = ps
			sessionDates = append(sessionDates, f.Date)
		}
		s := ps.facts[f.SessionID]
		if s == nil {
			s = &sessionFacts{}
			ps.facts[f.SessionID] = s
			ps.order = append(ps.order, f.SessionID)
		}
		if t := f.Counts.Tokens; t != nil && (s.tokens == nil || t.Input+t.Output > s.tokens.Input+s.tokens.Output) {
			s.tokens = t
			s.tokensByModel = f.Counts.TokensByModel
		}
		if secs := f.Counts.Seconds; secs != nil && (s.seconds == nil || secs.Human+secs.Agent > s.seconds.Human+s.seconds.Agent) {
			s.seconds = secs
		}
		key := TaskCostKey(f.Counts.TaskExec, f.keyed())
		if TaskCostRank(&key) > TaskCostRank(s.task) {
			s.task = &key
		}
		s.userMessages += f.Counts.UserMessages
	}

	foldCheckBuild(days, files)
	for _, date := range jsObjectOrder(dateOrder) {
		days.ObjAt(date).Set("sessions", float64(len(sessionsByDay[date].order)))
	}
	for _, date := range mapKeysInOrder(skillSessions, dateOrder) {
		b := skillSessions[date]
		for _, skill := range b.order {
			days.ObjAt(date).ObjAt("skillSessions").Set(skill, float64(len(b.sets[skill].order)))
		}
	}
	for _, date := range mapKeysInOrder(ruleSessions, dateOrder) {
		b := ruleSessions[date]
		for _, rule := range b.order {
			days.ObjAt(date).ObjAt("checkFindings").ObjAt(rule).Set("sessions", float64(len(b.sets[rule].order)))
		}
	}
	for _, date := range jsObjectOrder(sessionDates) {
		day := days.ObjAt(date)
		ps := perSession[date]
		for _, id := range ps.order {
			s := ps.facts[id]
			if s.tokens != nil {
				bumpOr(day, "tokensIn", s.tokens.Input)
				bumpOr(day, "tokensOut", s.tokens.Output)
				bumpOr(day, "tokenSessions", 1.0)
			}
			if s.tokensByModel != nil {
				if err := addCounters(day.ObjAt("tokensByModel"), s.tokensByModel, "tokensByModel"); err != nil {
					return nil, err
				}
			}
			if s.seconds != nil {
				bumpOr(day, "humanSeconds", s.seconds.Human)
				bumpOr(day, "agentSeconds", s.seconds.Agent)
			}
			costs := day.ObjAt("taskCost")
			cost := costs.ObjAt(*s.task)
			if cost == nil {
				cost = ObjOf("sessions", 0.0, "userMessages", 0.0)
				costs.Set(*s.task, cost)
			}
			bump(cost, "sessions", 1)
			bump(cost, "userMessages", s.userMessages)
			if s.tokens != nil {
				bumpOr(cost, "tokensIn", s.tokens.Input)
				bumpOr(cost, "tokensOut", s.tokens.Output)
			}
		}
	}
	return days, nil
}

// mapKeysInOrder is the dates a per-date map holds, in the order an object
// keyed by them iterates.
func mapKeysInOrder[V any](m map[string]V, order []string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, d := range order {
		if _, ok := m[d]; ok && !seen[d] {
			seen[d] = true
			keys = append(keys, d)
		}
	}
	return jsObjectOrder(keys)
}

// FoldDayFields merges a day-keyed map of already-computed fields into the
// day rows; a finite number is the only value a field takes.
func FoldDayFields(days, bySource *Obj) {
	for _, date := range bySource.Keys() {
		v, _ := bySource.Get(date)
		day := days.ObjAt(date)
		if day == nil {
			day = emptyDay()
			days.Set(date, day)
		}
		keys, vals := entriesOf(v)
		for i, field := range keys {
			if isFiniteNumber(vals[i]) {
				day.Set(field, vals[i])
			}
		}
	}
}

// QueueRecord is one closed work item the session half counts: its
// outcome, and the park kinds it collected, nil where its event listing
// could not be read.
type QueueRecord struct {
	Date, ClosedAt, Pack, Task, Outcome string
	Number                              any
	Parks                               []string
}

func dayRow(days *Obj, date string) *Obj {
	day := days.ObjAt(date)
	if day == nil {
		day = emptyDay()
		days.Set(date, day)
	}
	return day
}

// carryPrior copies each prior day's group onto the day rows while the
// day is inside its window.
func carryPrior(days, priorDays *Obj, group, today string, window float64) {
	for _, date := range priorDays.Keys() {
		row, _ := priorDays.Get(date)
		prior, _ := propOf(row, group)
		if !truthy(prior) {
			continue
		}
		if keys, _ := entriesOf(prior); len(keys) == 0 {
			continue
		}
		if !WithinTaskWindow(date, today, window) {
			continue
		}
		dayRow(days, date).Set(group, clone(prior))
	}
}

// FoldQueueOutcomes carries the prior days' queue and park rows forward
// inside the day window and appends the items that closed past the mark.
func FoldQueueOutcomes(days, priorDays *Obj, records []QueueRecord, today string) {
	for _, date := range priorDays.Keys() {
		if !WithinTaskWindow(date, today, DayWindowDays) {
			continue
		}
		row, _ := priorDays.Get(date)
		for _, group := range []string{"queue", "parks"} {
			prior, _ := propOf(row, group)
			if !truthy(prior) {
				continue
			}
			if keys, _ := entriesOf(prior); len(keys) == 0 {
				continue
			}
			dayRow(days, date).Set(group, clone(prior))
		}
	}
	for _, r := range records {
		day := dayRow(days, r.Date)
		id := r.Pack + "/" + r.Task
		if has(QueueOutcomes, r.Outcome) {
			q := day.Sub("queue")
			row := q.ObjAt(id)
			if row == nil {
				row = zeros(QueueOutcomes)
				q.Set(id, row)
			}
			bump(row, r.Outcome, 1)
		}
		if r.Parks == nil {
			continue
		}
		p := day.Sub("parks")
		row := p.ObjAt(id)
		if row == nil {
			row = zeros(UsageFields["parks"])
			p.Set(id, row)
		}
		seen := map[string]bool{}
		for _, kind := range r.Parks {
			if seen[kind] {
				continue
			}
			seen[kind] = true
			if row.Has(kind) {
				bump(row, kind, 1)
			}
		}
	}
}

// PrRecord is one merged pull request's durations, in hours; nil where
// the far end is unknown.
type PrRecord struct {
	Date                                      string
	Number                                    any
	LeadHours, IssueLeadHours, SessionToMerge *float64
}

// FoldPrs carries the prior days' PR rows forward inside the day window
// and files each newly merged one under the day it merged.
func FoldPrs(days, priorDays *Obj, records []PrRecord, today string) {
	carryPrior(days, priorDays, "prs", today, DayWindowDays)
	for _, r := range records {
		day := dayRow(days, r.Date)
		row := NewObj()
		for i, h := range []*float64{r.LeadHours, r.IssueLeadHours, r.SessionToMerge} {
			if h != nil && isFiniteNumber(*h) {
				row.Set(UsageFields["prs"][i], *h)
			}
		}
		day.Sub("prs").Set(jsString(r.Number), row)
	}
}

// HourWindowHours is how long an hour row lives.
const HourWindowHours = 72

// WithinHourWindow reports an hour key inside the window that ends at
// now's hour.
func WithinHourWindow(key, now string, hours float64) bool {
	age := (parseDate(sliceUnits(now, 0, 13)+":00:00Z") - parseDate(key+":00:00Z")) / 3600000
	return !math.IsNaN(age) && !math.IsInf(age, 0) && age >= 0 && age < hours
}

// Run is one completed workflow run the listings read.
type Run struct {
	ID         any
	StartedAt  string
	Workflow   string
	Conclusion any
}

// CaptureHour is the capture files' view of one hour: the distinct
// sessions that captured in it and what they executed.
type CaptureHour struct {
	Agentic  float64
	TaskExec *Obj
}

// FoldHours is the hour rows: run counts appended onto the prior rows,
// the capture-derived counts recomputed over them.
func FoldHours(prior *Obj, runs []Run, captures *Obj, now string) *Obj {
	hours := NewObj()
	for _, key := range prior.Keys() {
		if !WithinHourWindow(key, now, HourWindowHours) {
			continue
		}
		row, _ := prior.Get(key)
		r := spread(emptyHour(), spreadOf(clone(row)))
		r.Set("taskExec", NewObj())
		hours.Set(key, r)
	}
	for _, run := range runs {
		key := HourKey(run.StartedAt)
		if !WithinHourWindow(key, now, HourWindowHours) {
			continue
		}
		row := hours.ObjAt(key)
		if row == nil {
			row = emptyHour()
			hours.Set(key, row)
		}
		switch run.Workflow {
		case "scheduler":
			bump(row, "scheduler", 1)
		case "executor":
			bump(row, "executor", 1)
		}
		switch run.Conclusion {
		case "failure", "timed_out", "startup_failure":
			bump(row, "failed", 1)
		}
	}
	for _, key := range captures.Keys() {
		if !WithinHourWindow(key, now, HourWindowHours) {
			continue
		}
		v, _ := captures.Get(key)
		c := v.(*CaptureHour)
		row := hours.ObjAt(key)
		if row == nil {
			row = emptyHour()
			hours.Set(key, row)
		}
		row.Set("agentic", c.Agentic)
		row.Set("taskExec", cloneObj(c.TaskExec))
	}
	return hours
}

// CaptureHours files each capture's session under the hour its name
// stamps.
func CaptureHours(files []CaptureFile) *Obj {
	out := NewObj()
	sessions := map[string]*orderedSet{}
	for _, f := range files {
		if f.Stamp == "" {
			continue
		}
		key := HourKey(f.Stamp)
		v, ok := out.Get(key)
		if !ok {
			v = &CaptureHour{TaskExec: NewObj()}
			out.Set(key, v)
		}
		if sessions[key] == nil {
			sessions[key] = &orderedSet{}
		}
		sessions[key].add(f.SessionID)
		_ = addCounters(v.(*CaptureHour).TaskExec, f.Counts.TaskExec, "taskExec")
	}
	for key, set := range sessions {
		v, _ := out.Get(key)
		v.(*CaptureHour).Agentic = float64(len(set.order))
	}
	return out
}

// The day tier's two windows: the retired slot scheduler's task rows, and
// every day row.
const (
	TaskDayWindowDays = 14
	DayWindowDays     = 30
)

// WithinTaskWindow reports a date inside the window of days that ends
// today.
func WithinTaskWindow(date, today string, days float64) bool {
	age := (parseDate(today+"T00:00:00Z") - parseDate(date+"T00:00:00Z")) / 86400000
	return !math.IsNaN(age) && !math.IsInf(age, 0) && age >= 0 && age < days
}

// CarryTaskRuns ages the retired slot scheduler's task rows out: carried
// while inside their window, never appended to.
func CarryTaskRuns(days, priorDays *Obj, today string) {
	carryPrior(days, priorDays, "tasks", today, TaskDayWindowDays)
}

// IsoWeek is the ISO-8601 week a UTC date falls in, YYYY-Www.
func IsoWeek(date string) string {
	d := parseDate(date + "T00:00:00Z")
	if math.IsNaN(d) {
		return "NaN-WNaN"
	}
	const dayMs = 86400000
	thursday := d + float64(3-(utcDay(d)+6)%7)*dayMs
	year := utcYear(thursday)
	first := float64(daysFromCivil(year, 1, 4)) * dayMs
	first += float64(3-(utcDay(first)+6)%7) * dayMs
	week := 1 + jsRound((thursday-first)/(7*dayMs))
	w := jsString(week)
	if len(w) < 2 {
		w = "0" + w
	}
	return strconv.Itoa(year) + "-W" + w
}

// DaysToFold are the days that closed after the watermark and before
// today, in order.
func DaysToFold(days *Obj, foldedThrough any, today string) []string {
	var out []string
	for _, d := range days.Keys() {
		if jsLess(d, today) && (!truthy(foldedThrough) || greaterThan(d, foldedThrough)) {
			out = append(out, d)
		}
	}
	return jsSort(out)
}

// greaterThan is a > b for a string a.
func greaterThan(a string, b any) bool {
	p := toPrimitive(b)
	if s, ok := p.(string); ok {
		return jsLess(s, a)
	}
	x, y := stringToNumber(a), toNumber(p)
	return x > y
}

// AddDayToWeek adds one closed day row into its week row; a day with no
// opinion on a field adds nothing to it.
func AddDayToWeek(week any, day *Obj) (*Obj, error) {
	var w *Obj
	switch x := week.(type) {
	case nil:
		w = ObjOf("skillLoads", NewObj(), "checks", NewObj(), "checkFindings", NewObj())
	case *Obj:
		w = x
	default:
		return nil, typeError("days", week)
	}
	bumpOr(w, "days", 1.0)
	for _, field := range UsageFields["week"] {
		if field == "days" {
			continue
		}
		src := field
		if f, ok := weekFromDay[field]; ok {
			src = f
		}
		if v, _ := day.Get(src); isFiniteNumber(v) {
			bumpOr(w, field, v)
		}
	}
	for _, m := range BareMaps {
		_, into, _ := subOf(w, m)
		from, _ := day.Get(m)
		if err := addLoads(into, from); err != nil {
			return nil, err
		}
	}
	for _, g := range CounterGroups {
		_, into, _ := subOf(w, g)
		from, _ := day.Get(g)
		if err := addCounters(into, from, g); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// FoldIn is one session fold's inputs.
type FoldIn struct {
	Files              []CaptureFile
	Prior              UsageFile
	Today              string
	Now                *string
	RunsFoldedThrough  any
	QueueRecords       []QueueRecord
	QueueFoldedThrough any
	PrRecords          []PrRecord
	PrsFoldedThrough   any
	Runs               []Run
	DayFields          *Obj
	Generated          any
}

func orObj(o *Obj) *Obj {
	if o == nil {
		return NewObj()
	}
	return o
}

func firstOf(vs ...any) any {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}
	return nil
}

// FoldUsage folds one run of the session half.
func FoldUsage(in FoldIn) (UsageFile, error) {
	days, err := FoldDays(in.Files)
	if err != nil {
		return UsageFile{}, err
	}
	priorDays := orObj(in.Prior.Days)
	CarryTaskRuns(days, priorDays, in.Today)
	FoldQueueOutcomes(days, priorDays, in.QueueRecords, in.Today)
	FoldPrs(days, priorDays, in.PrRecords, in.Today)
	FoldDayFields(days, orObj(in.DayFields))
	weeks := cloneObj(in.Prior.Weeks)
	foldedThrough := in.Prior.FoldedThrough
	for _, date := range DaysToFold(days, foldedThrough, in.Today) {
		key := IsoWeek(date)
		prior, _ := weeks.Get(key)
		w, err := AddDayToWeek(prior, days.ObjAt(date))
		if err != nil {
			return UsageFile{}, err
		}
		weeks.Set(key, w)
		foldedThrough = date
	}
	for _, date := range days.Keys() {
		if !WithinTaskWindow(date, in.Today, DayWindowDays) {
			days.Delete(date)
		}
	}
	now := in.Today + "T23:59:59Z"
	if in.Now != nil {
		now = *in.Now
	}
	return UsageFile{
		Generated:          in.Generated,
		FoldedThrough:      foldedThrough,
		QueueFoldedThrough: firstOf(in.QueueFoldedThrough, in.Prior.QueueFoldedThrough),
		PrsFoldedThrough:   firstOf(in.PrsFoldedThrough, in.Prior.PrsFoldedThrough),
		Hours:              sortKeys(FoldHours(orObj(in.Prior.Hours), in.Runs, CaptureHours(in.Files), now)),
		RunsFoldedThrough:  firstOf(in.RunsFoldedThrough, in.Prior.RunsFoldedThrough),
		Days:               sortKeys(days),
		Weeks:              sortKeys(weeks),
	}, nil
}
