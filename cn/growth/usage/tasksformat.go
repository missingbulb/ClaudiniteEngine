package usage

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
)

// TasksUsagePath is the machinery half's rolling file, and
// LegacyTasksUsagePath where it lived before.
const (
	TasksUsagePath       = ".claudinite/usage/task-runs-and-costs.json"
	LegacyTasksUsagePath = ".claudinite/local/tasks-usage.GENERATED.json"
)

// TasksUsageVersion is the machinery file's format version.
const TasksUsageVersion = 1

// RunPhases are every phase a cost record times, scheduler's then
// executor's.
var RunPhases = append(append([]string{}, queue.RunPhases["scheduler"]...), queue.RunPhases["executor"]...)

// The machinery file's vocabularies.
var (
	LatencyFields  = []string{"tickToItemMinutes", "itemToPickMinutes", "pickToHandOffMinutes", "handOffToConvergeMinutes"}
	WorkflowFields = []string{"runs", "jobs", "minutesBilled", "spend"}
	RunFields      = append([]string{"apiCalls"}, RunPhases...)
	BucketFields   = append(append([]string{}, WorkflowFields...), RunFields...)
)

// TasksFieldOrder is the order the machinery file's fields header lists
// its vocabularies in.
var TasksFieldOrder = []string{"hour", "day", "week", "workflows", "runCosts", "queue", "parks", "latency"}

// TasksFields are the machinery file's counter vocabularies.
var TasksFields = map[string][]string{
	"hour":      BucketFields,
	"day":       BucketFields,
	"week":      append([]string{"days"}, BucketFields...),
	"workflows": WorkflowFields,
	"runCosts":  RunFields,
	"queue":     QueueOutcomes,
	"parks":     workitem.ParkKinds,
	"latency":   LatencyFields,
}

// The machinery file's counter groups: all of them on a day row, the live
// cost picture on an hour row, and on a week row every group whose keys
// do not grow per run.
var (
	TasksCounterGroups = []string{"workflows", "runCosts", "queue", "parks", "latency"}
	HourGroups         = []string{"workflows", "runCosts"}
	WeekGroups         = []string{"workflows", "queue", "parks", "latency"}
)

func encodeTasksRow(row *Obj, totals []string, groups []string) *Obj {
	out := ObjOf("totals", encodeCounters(row, totals))
	for _, g := range groups {
		v, _ := row.Get(g)
		if !nonEmpty(v) {
			continue
		}
		group := v.(*Obj)
		enc := NewObj()
		for _, k := range sortedKeys(group) {
			r, _ := group.Get(k)
			enc.Set(k, encodeCounters(r, TasksFields[g]))
		}
		out.Set(g, enc)
	}
	return out
}

func decodeTasksRow(row any, totals []string, fields map[string][]string, groups []string) *Obj {
	t, _ := propOf(row, "totals")
	out := spread(decodeCounters(t, totals))
	for _, g := range groups {
		v, _ := propOf(row, g)
		keys, vals := entriesOf(v)
		dec := NewObj()
		for i, k := range keys {
			dec.Set(k, decodeCounters(vals[i], fields[g]))
		}
		out.Set(g, dec)
	}
	return out
}

// TasksUsageFile is the machinery file in named counters.
type TasksUsageFile struct {
	Generated, FoldedThrough, RunsFoldedThrough, QueueFoldedThrough any
	// MinuteRate is the rate every spend was computed at, nil for none.
	MinuteRate         any
	Hours, Days, Weeks *Obj
}

// EncodeTasksUsage is a fold as the file's object.
func EncodeTasksUsage(f TasksUsageFile) *Obj {
	or := func(o *Obj) *Obj {
		if o == nil {
			return NewObj()
		}
		return o
	}
	fields := NewObj()
	for _, k := range TasksFieldOrder {
		fields.Set(k, strList(TasksFields[k]))
	}
	tier := func(rows *Obj, totals string, groups []string) *Obj {
		out := NewObj()
		for _, k := range rows.Keys() {
			v, _ := rows.Get(k)
			r, _ := v.(*Obj)
			out.Set(k, encodeTasksRow(r, TasksFields[totals], groups))
		}
		return out
	}
	return ObjOf(
		"version", float64(TasksUsageVersion),
		"generated", f.Generated,
		"foldedThrough", f.FoldedThrough,
		"runsFoldedThrough", f.RunsFoldedThrough,
		"queueFoldedThrough", f.QueueFoldedThrough,
		"minuteRate", f.MinuteRate,
		"fields", fields,
		"hours", tier(or(f.Hours), "hour", HourGroups),
		"days", tier(or(f.Days), "day", TasksCounterGroups),
		"weeks", tier(or(f.Weeks), "week", WeekGroups),
	)
}

// RenderTasksUsageFile is the file's text, one line per row.
func RenderTasksUsageFile(file *Obj) string {
	version, _ := file.Get("version")
	lines := []string{"{", `  "version": ` + Stringify(version) + ","}
	for _, k := range []string{"generated", "foldedThrough", "runsFoldedThrough", "queueFoldedThrough", "minuteRate"} {
		lines = append(lines, markLine(file, k)+",")
	}
	for _, k := range []string{"fields", "hours", "days"} {
		v, _ := file.Get(k)
		renderRows(&lines, k, v, ",")
	}
	v, _ := file.Get("weeks")
	renderRows(&lines, "weeks", v, "")
	return strings.Join(append(lines, "}", ""), "\n")
}

// DecodeTasksUsage is a parsed file in named counters; a value that is not
// an object reads as an empty file.
func DecodeTasksUsage(file any) TasksUsageFile {
	switch file.(type) {
	case *Obj, []any:
	default:
		return TasksUsageFile{Hours: NewObj(), Days: NewObj(), Weeks: NewObj()}
	}
	mark := func(k string) any { v, ok := propOf(file, k); return nullish(v, ok) }
	var rate any
	if r, ok := propOf(file, "minuteRate"); ok {
		if n, isNum := r.(float64); isNum {
			rate = n
		}
	}
	fields := fieldsOf(file, TasksFieldOrder, TasksFields)
	rows := func(key, totals string, groups []string) *Obj {
		v, _ := propOf(file, key)
		keys, vals := entriesOf(v)
		out := NewObj()
		for i, k := range keys {
			out.Set(k, decodeTasksRow(vals[i], fields[totals], fields, groups))
		}
		return out
	}
	return TasksUsageFile{
		Generated: mark("generated"), FoldedThrough: mark("foldedThrough"),
		RunsFoldedThrough: mark("runsFoldedThrough"), QueueFoldedThrough: mark("queueFoldedThrough"),
		MinuteRate: rate,
		Hours:      rows("hours", "hour", HourGroups),
		Days:       rows("days", "day", TasksCounterGroups),
		Weeks:      rows("weeks", "week", WeekGroups),
	}
}

// ReadRunsMark is the run mark the checkout's machinery file carries, as
// text: nil where nothing has been folded, and whether it was a string. A
// file that does not parse at its path is read at the legacy path; one
// that parses stands, mark or not.
func ReadRunsMark(root string) (mark *string, isText bool) {
	for _, p := range []string{TasksUsagePath, LegacyTasksUsagePath} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		v, err := ParseJSON(string(raw))
		if err != nil {
			continue
		}
		m := DecodeTasksUsage(v).RunsFoldedThrough
		if !truthy(m) {
			return nil, false
		}
		s := jsString(m)
		_, isText = m.(string)
		return &s, isText
	}
	return nil, false
}
