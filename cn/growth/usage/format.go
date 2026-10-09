package usage

import (
	"strings"
	"unicode/utf16"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
)

// UsagePath is the session half's rolling file, and LegacyUsagePath where
// it lived before; the delivery moves it, bytes unchanged.
const (
	UsagePath       = ".claudinite/usage/sessions-and-elements.json"
	LegacyUsagePath = ".claudinite/local/usage.GENERATED.json"
)

// UsageVersion is the session file's format version.
const UsageVersion = 4

// QueueOutcomes are the queue's outcome words a closed item decodes to,
// plus none for one closed wearing no outcome label.
var QueueOutcomes = []string{"done", "delivered", "obsolete", "none"}

// UsageFieldOrder is the order the session file's fields header lists its
// vocabularies in, and UsageFields each vocabulary in its tuple's order.
var UsageFieldOrder = []string{
	"day", "week", "hour", "checks", "checkFindings", "skillLoadsBy", "triggerFires", "guardFires",
	"checkTiming", "buildSessions", "buildWaits", "tokensByModel", "prs", "taskCost", "parks", "tasks",
	"taskExec", "queue",
}

// UsageFields are the session file's counter vocabularies.
var UsageFields = map[string][]string{
	"day": {"captures", "merges", "sessions", "userMessages", "userCommands",
		"tokensIn", "tokensOut", "tokenSessions",
		"commits", "linesAdded", "linesRemoved", "releases",
		"humanSeconds", "agentSeconds"},
	"week": {"days", "captures", "merges", "sessionDays", "userMessages", "userCommands",
		"tokensIn", "tokensOut", "tokenSessions",
		"commits", "linesAdded", "linesRemoved", "releases",
		"humanSeconds", "agentSeconds"},
	"hour":          {"scheduler", "executor", "agentic", "failed"},
	"checks":        {"runs", "failures", "errors", "blocking", "advisory", "ciRuns", "ciFailures"},
	"checkFindings": {"blocking", "advisory", "sessions", "persisted", "relent"},
	"skillLoadsBy":  LoadCauses,
	"triggerFires":  {"fired", "followed"},
	"guardFires":    {"blocking", "advisory"},
	"checkTiming":   {"runs", "totalMs", "maxMs"},
	"buildSessions": {"cached", "started", "compiledMs", "compiledOk", "waits", "waitMs", "waitMaxMs"},
	"buildWaits":    {"waits", "sessions", "totalMs", "maxMs", "timeouts"},
	"tokensByModel": {"input", "cacheRead", "cacheCreate", "output"},
	"prs":           {"leadHours", "issueLeadHours", "sessionToMergeHours"},
	"taskCost":      {"sessions", "tokensIn", "tokensOut", "userMessages"},
	"parks":         workitem.ParkKinds,
	"tasks":         queue.TaskRunOutcomes,
	"taskExec":      queue.TaskExecStatuses,
	"queue":         QueueOutcomes,
}

// CaptureDayFields are the day fields the capture files answer, always
// present on a day row; every other day field comes from a source that
// can be absent and is then no key at all.
var CaptureDayFields = []string{"captures", "merges", "sessions", "userMessages", "userCommands"}

// weekFromDay is the day field a week field sums, where the names differ.
var weekFromDay = map[string]string{"sessionDays": "sessions"}

// CounterGroups are a row's sub-maps carrying a tuple per key.
var CounterGroups = []string{
	"checks", "checkFindings", "tasks", "taskExec", "queue", "tokensByModel", "prs", "taskCost", "parks",
	"skillLoadsBy", "triggerFires", "guardFires", "checkTiming", "buildSessions", "buildWaits",
}

// BareMaps are a row's sub-maps whose values are bare numbers.
var BareMaps = []string{"skillLoads", "skillSessions", "skillBlocks", "moments", "toolCalls", "skillCaught"}

// maxFields are the counter fields folded by maximum rather than by sum.
var maxFields = map[string][]string{
	"checkTiming":   {"maxMs"},
	"buildSessions": UsageFields["buildSessions"],
	"buildWaits":    {"maxMs"},
}

// HumanSecondsCap bounds the gap before a human turn that counts as the
// person's time.
const HumanSecondsCap = 600

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func strList(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func nonEmpty(v any) bool {
	o, ok := v.(*Obj)
	return ok && o != nil && o.Len() > 0
}

// entriesOf is Object.entries(v ?? {}).
func entriesOf(v any) ([]string, []any) {
	switch x := v.(type) {
	case *Obj:
		keys := x.Keys()
		vals := make([]any, len(keys))
		for i, k := range keys {
			vals[i], _ = x.Get(k)
		}
		return keys, vals
	case []any:
		keys := make([]string, len(x))
		for i := range x {
			keys[i] = itoa(i)
		}
		return keys, x
	case string:
		units := utf16.Encode([]rune(x))
		keys := make([]string, len(units))
		vals := make([]any, len(units))
		for i, u := range units {
			keys[i] = itoa(i)
			vals[i] = string(utf16.Decode([]uint16{u}))
		}
		return keys, vals
	}
	return nil, nil
}

// spreadOf is {...(v ?? {})}.
func spreadOf(v any) *Obj {
	out := NewObj()
	keys, vals := entriesOf(v)
	for i, k := range keys {
		out.Set(k, vals[i])
	}
	return out
}

// encodeCounters is one counter row as a tuple: an absent field is null.
func encodeCounters(row any, fields []string) []any {
	out := make([]any, len(fields))
	for i, f := range fields {
		if v, ok := propOf(row, f); ok && v != nil {
			out[i] = v
		}
	}
	return out
}

// decodeCounters is a tuple as a counter row: a null slot, or one past a
// short tuple's end, is no key.
func decodeCounters(tuple any, fields []string) *Obj {
	row := NewObj()
	arr, isArr := tuple.([]any)
	for i, f := range fields {
		if !isArr || i >= len(arr) {
			continue
		}
		if n, ok := arr[i].(float64); ok {
			row.Set(f, n)
		}
	}
	return row
}

// encodeRow is a named row as the file's tuples: totals, then each
// non-empty bare map and counter group, keys sorted.
func encodeRow(row *Obj, totals []string) *Obj {
	out := ObjOf("totals", encodeCounters(row, totals))
	for _, m := range BareMaps {
		if v, _ := row.Get(m); nonEmpty(v) {
			out.Set(m, sortKeys(v.(*Obj)))
		}
	}
	for _, g := range CounterGroups {
		v, _ := row.Get(g)
		if !nonEmpty(v) {
			continue
		}
		group := v.(*Obj)
		enc := NewObj()
		for _, k := range sortedKeys(group) {
			r, _ := group.Get(k)
			enc.Set(k, encodeCounters(r, UsageFields[g]))
		}
		out.Set(g, enc)
	}
	return out
}

// decodeRow is a file row as a named row, against the file's own
// vocabularies; every bare map and group comes back present.
func decodeRow(row any, totals []string, fields map[string][]string) *Obj {
	t, _ := propOf(row, "totals")
	out := spread(decodeCounters(t, totals))
	for _, m := range BareMaps {
		v, _ := propOf(row, m)
		out.Set(m, spreadOf(v))
	}
	for _, g := range CounterGroups {
		vocab := fields[g]
		v, _ := propOf(row, g)
		keys, vals := entriesOf(v)
		dec := NewObj()
		for i, k := range keys {
			dec.Set(k, decodeCounters(vals[i], vocab))
		}
		out.Set(g, dec)
	}
	return out
}

// vocabularyOf is a file's declared vocabulary for one key, the module's
// where it declares none.
func vocabularyOf(file any, key string, defaults map[string][]string) []string {
	declared, ok := path(file, "fields", key)
	if arr, isArr := declared.([]any); ok && isArr {
		out := make([]string, len(arr))
		for i, f := range arr {
			out[i] = jsString(f)
		}
		return out
	}
	return defaults[key]
}

func fieldsOf(file any, order []string, defaults map[string][]string) map[string][]string {
	out := map[string][]string{}
	for _, k := range order {
		out[k] = vocabularyOf(file, k, defaults)
	}
	return out
}

func isTupleFormat(file any) bool {
	v, ok := propOf(file, "version")
	if !ok || v == nil {
		v = 1.0
	}
	return toNumber(v) >= 2
}

// HourKey is the UTC hour an instant falls in, YYYY-MM-DDTHH.
func HourKey(iso string) string { return sliceUnits(iso, 0, 13) }

// sliceUnits is s.slice(from, to) over UTF-16 code units.
func sliceUnits(s string, from, to int) string {
	u := utf16.Encode([]rune(s))
	if to > len(u) {
		to = len(u)
	}
	if from > to {
		return ""
	}
	return string(utf16.Decode(u[from:to]))
}

// UsageFile is the session file in named counters: its watermarks, each
// whatever value the file held (nil for null), and its three tiers.
type UsageFile struct {
	Generated, FoldedThrough, RunsFoldedThrough, QueueFoldedThrough, PrsFoldedThrough any
	Hours, Days, Weeks                                                                *Obj
}

func (f UsageFile) tiers() (hours, days, weeks *Obj) {
	or := func(o *Obj) *Obj {
		if o == nil {
			return NewObj()
		}
		return o
	}
	return or(f.Hours), or(f.Days), or(f.Weeks)
}

// EncodeUsage is a fold as the file's object.
func EncodeUsage(f UsageFile) *Obj {
	hours, days, weeks := f.tiers()
	fields := NewObj()
	for _, k := range UsageFieldOrder {
		fields.Set(k, strList(UsageFields[k]))
	}
	tier := func(rows *Obj, totals string) *Obj {
		out := NewObj()
		for _, k := range rows.Keys() {
			v, _ := rows.Get(k)
			r, _ := v.(*Obj)
			out.Set(k, encodeRow(r, UsageFields[totals]))
		}
		return out
	}
	return ObjOf(
		"version", float64(UsageVersion),
		"generated", f.Generated,
		"foldedThrough", f.FoldedThrough,
		"runsFoldedThrough", f.RunsFoldedThrough,
		"queueFoldedThrough", f.QueueFoldedThrough,
		"prsFoldedThrough", f.PrsFoldedThrough,
		"fields", fields,
		"caps", ObjOf("humanSeconds", float64(HumanSecondsCap)),
		"hours", tier(hours, "hour"),
		"days", tier(days, "day"),
		"weeks", tier(weeks, "week"),
	)
}

func markLine(file *Obj, key string) string {
	v, ok := file.Get(key)
	return `  "` + key + `": ` + Stringify(nullish(v, ok))
}

func renderRows(b *[]string, name string, v any, end string) {
	o, _ := v.(*Obj)
	if o.Len() == 0 {
		*b = append(*b, `  "`+name+`": {}`+end)
		return
	}
	rows := make([]string, 0, o.Len())
	for _, k := range o.Keys() {
		r, _ := o.Get(k)
		rows = append(rows, "    "+Stringify(k)+": "+Stringify(r))
	}
	*b = append(*b, `  "`+name+`": {`, strings.Join(rows, ",\n"), "  }"+end)
}

// RenderUsageFile is the file's text, one line per row.
func RenderUsageFile(file *Obj) string {
	version, _ := file.Get("version")
	lines := []string{"{", `  "version": ` + Stringify(version) + ","}
	for _, k := range []string{"generated", "foldedThrough", "runsFoldedThrough", "queueFoldedThrough", "prsFoldedThrough"} {
		lines = append(lines, markLine(file, k)+",")
	}
	for _, k := range []string{"fields", "caps", "hours", "days"} {
		v, _ := file.Get(k)
		renderRows(&lines, k, v, ",")
	}
	v, _ := file.Get("weeks")
	renderRows(&lines, "weeks", v, "")
	return strings.Join(append(lines, "}", ""), "\n")
}

var stampLine = jsPattern(`^\s*"generated":`)

// WithoutStamp is a file's text without its generated line, the one line
// the unchanged-compare ignores.
func WithoutStamp(text string) string {
	lines := strings.Split(text, "\n")
	kept := lines[:0:0]
	for _, l := range lines {
		if !stampLine.MatchString(l) {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

func objOrEmpty(v any, ok bool) *Obj {
	if o, isObj := v.(*Obj); ok && isObj && o != nil {
		return o
	}
	return NewObj()
}

// DecodeUsage is a parsed file in named counters. A version-1 file is
// already named and reads as itself; a value that is not an object reads
// as an empty file.
func DecodeUsage(file any) UsageFile {
	empty := UsageFile{Hours: NewObj(), Days: NewObj(), Weeks: NewObj()}
	switch file.(type) {
	case *Obj, []any:
	default:
		return empty
	}
	mark := func(k string) any { v, ok := propOf(file, k); return nullish(v, ok) }
	out := UsageFile{Generated: mark("generated"), FoldedThrough: mark("foldedThrough"),
		RunsFoldedThrough: mark("runsFoldedThrough"), QueueFoldedThrough: mark("queueFoldedThrough"),
		PrsFoldedThrough: mark("prsFoldedThrough")}
	if !isTupleFormat(file) {
		out.Hours = NewObj()
		out.Days = objOrEmpty(propOf(file, "days"))
		out.Weeks = objOrEmpty(propOf(file, "weeks"))
		return out
	}
	fields := fieldsOf(file, UsageFieldOrder, UsageFields)
	rows := func(key, totals string) *Obj {
		v, _ := propOf(file, key)
		keys, vals := entriesOf(v)
		out := NewObj()
		for i, k := range keys {
			out.Set(k, decodeRow(vals[i], fields[totals], fields))
		}
		return out
	}
	out.Hours, out.Days, out.Weeks = rows("hours", "hour"), rows("days", "day"), rows("weeks", "week")
	return out
}

func itoa(n int) string {
	return jsString(float64(n))
}
