package usage

import (
	"math"
	"regexp"
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
)

// What the engine's checks build cost each session, read off its own
// breadcrumbs: `[cn] build cached|started|compiled <outcome> <ms>ms` and
// `[cn] buildwait <event> <outcome> <ms>ms`. A session with no build line
// ran an engine that leaves none, which is not a session that built
// nothing.

var (
	reBuild = regexp.MustCompile(`\[cn\] build (cached|started|compiled) (ok|error|timeout) (\d+)ms`)
	reWait  = regexp.MustCompile(`\[cn\] buildwait ([a-z-]+) (ok|error|timeout) (\d+)ms`)
)

// CheckBuild is one capture's build record and how many breadcrumb lines
// it was read from, the measure of which capture of a session is the most
// complete.
type CheckBuild struct {
	Record *Obj
	Lines  int
}

// ReadCheckBuild is one capture's build record, nil where it carries no
// build line; a line recorded twice counts once.
func ReadCheckBuild(entries []any) *CheckBuild {
	seen := map[string]bool{}
	out := ObjOf("cached", 0.0, "started", 0.0, "waits", NewObj())
	waits := out.ObjAt("waits")
	lines := 0
	for _, entry := range entries {
		for _, text := range EntryText(entry) {
			for _, m := range reBuild.FindAllStringSubmatch(text, -1) {
				if seen[m[0]] {
					continue
				}
				seen[m[0]] = true
				lines++
				switch m[1] {
				case "cached":
					bump(out, "cached", 1)
				case "started":
					bump(out, "started", 1)
				default:
					ms, _ := out.Get("compiledMs")
					out.Set("compiledMs", jsMax(orZero(ms, ms != nil), stringToNumber(m[3])))
					ok, has := out.Get("compiledOk")
					okNow := 0.0
					if m[2] == "ok" {
						okNow = 1
					}
					if !has {
						ok = 1.0
					}
					out.Set("compiledOk", jsMin(ok, okNow))
				}
			}
			for _, m := range reWait.FindAllStringSubmatch(text, -1) {
				if seen[m[0]] {
					continue
				}
				seen[m[0]] = true
				lines++
				ms := stringToNumber(m[3])
				row := waits.ObjAt(m[1])
				if row == nil {
					row = ObjOf("waits", 0.0, "totalMs", 0.0, "maxMs", 0.0, "timeouts", 0.0)
					waits.Set(m[1], row)
				}
				bump(row, "waits", 1)
				bump(row, "totalMs", ms)
				mx, _ := row.Get("maxMs")
				row.Set("maxMs", jsMax(mx, ms))
				if m[2] == "timeout" {
					bump(row, "timeouts", 1)
				}
			}
		}
	}
	if lines == 0 {
		return nil
	}
	return &CheckBuild{Record: out, Lines: lines}
}

// buildSessionRow is a session's row in the day tier: its build, and its
// waits summed over events.
func buildSessionRow(record *Obj) *Obj {
	cached, _ := record.Get("cached")
	started, _ := record.Get("started")
	row := ObjOf("cached", cached, "started", started)
	if ms, ok := record.Get("compiledMs"); ok {
		okv, _ := record.Get("compiledOk")
		row.Set("compiledMs", ms)
		row.Set("compiledOk", okv)
	}
	var n, total any = 0.0, 0.0
	maxMs := 0.0
	_, vals := entriesOf(record.ObjAt("waits"))
	for _, v := range vals {
		w, _ := propOf(v, "waits")
		t, _ := propOf(v, "totalMs")
		m, _ := propOf(v, "maxMs")
		n, total = plus(n, w), plus(total, t)
		maxMs = jsMax(maxMs, m)
	}
	row.Set("waits", n)
	row.Set("waitMs", total)
	row.Set("waitMaxMs", maxMs)
	return row
}

// foldCheckBuild files each (day, session)'s most complete build record
// into the day rows, and its waits by event.
func foldCheckBuild(days *Obj, files []CaptureFile) {
	type best struct {
		order   []string
		records map[string]*CheckBuild
	}
	byDate := map[string]*best{}
	var dates []string
	for _, f := range files {
		rec := f.Counts.CheckBuild
		if rec == nil {
			continue
		}
		b := byDate[f.Date]
		if b == nil {
			b = &best{records: map[string]*CheckBuild{}}
			byDate[f.Date] = b
			dates = append(dates, f.Date)
		}
		held, ok := b.records[f.SessionID]
		if !ok {
			b.order = append(b.order, f.SessionID)
		}
		if !ok || rec.Lines > held.Lines {
			b.records[f.SessionID] = rec
		}
	}
	for _, date := range jsObjectOrder(dates) {
		day := days.ObjAt(date)
		b := byDate[date]
		for _, session := range b.order {
			rec := b.records[session].Record
			day.Sub("buildSessions").Set(session, buildSessionRow(rec))
			waits := rec.ObjAt("waits")
			for _, event := range waits.Keys() {
				w := waits.ObjAt(event)
				row := day.Sub("buildWaits").ObjAt(event)
				if row == nil {
					row = ObjOf("waits", 0.0, "sessions", 0.0, "totalMs", 0.0, "maxMs", 0.0, "timeouts", 0.0)
					day.Sub("buildWaits").Set(event, row)
				}
				wv, _ := w.Get("waits")
				tv, _ := w.Get("totalMs")
				mv, _ := w.Get("maxMs")
				to, _ := w.Get("timeouts")
				bump(row, "waits", toNumber(wv))
				bump(row, "sessions", 1)
				bump(row, "totalMs", toNumber(tv))
				cur, _ := row.Get("maxMs")
				row.Set("maxMs", jsMax(cur, mv))
				bump(row, "timeouts", toNumber(to))
			}
		}
	}
}

// jsObjectOrder is the order an object's keys take when inserted in
// order: the canonical array indices first, ascending.
func jsObjectOrder(keys []string) []string {
	o := NewObj()
	for _, k := range keys {
		o.Set(k, true)
	}
	return o.Keys()
}

func median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.SliceStable(s, func(i, j int) bool { return s[i]-s[j] < 0 })
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return jsRound((s[mid-1] + s[mid]) / 2)
}

// summarizeWeek is one week's build figures, nil where no session
// reported a build in it.
func summarizeWeek(week any) *Obj {
	bs, _ := propOf(week, "buildSessions")
	_, sessions := entriesOf(bs)
	if len(sessions) == 0 {
		return nil
	}
	prop := func(s any, k string) any { v, _ := propOf(s, k); return v }
	isNum := func(s any, k string) bool { _, ok := prop(s, k).(float64); return ok }
	gt0 := func(s any, k string) bool {
		v, ok := propOf(s, k)
		return ok && toNumber(v) > 0
	}
	count := func(keep func(any) bool, from []any) float64 {
		n := 0.0
		for _, s := range from {
			if keep(s) {
				n++
			}
		}
		return n
	}
	var compiled, waited []any
	for _, s := range sessions {
		if isNum(s, "compiledMs") {
			compiled = append(compiled, s)
		}
		if gt0(s, "waits") {
			waited = append(waited, s)
		}
	}
	isOne := func(s any) bool {
		v, _ := prop(s, "compiledOk").(float64)
		_, ok := prop(s, "compiledOk").(float64)
		return ok && v == 1
	}
	out := ObjOf(
		"sessions", float64(len(sessions)),
		"builds", ObjOf(
			"cached", count(func(s any) bool { return gt0(s, "cached") }, sessions),
			"compiledOk", count(isOne, compiled),
			"compiledError", count(func(s any) bool { return !isOne(s) }, compiled),
			"unreported", count(func(s any) bool { return gt0(s, "started") && !isNum(s, "compiledMs") }, sessions),
		),
	)
	if len(compiled) > 0 {
		ms := make([]float64, len(compiled))
		mx := math.Inf(-1)
		for i, s := range compiled {
			ms[i] = prop(s, "compiledMs").(float64)
			mx = jsMax(mx, ms[i])
		}
		out.Set("compiled", ObjOf("count", float64(len(ms)), "medianMs", median(ms), "maxMs", mx))
	}
	var total any = 0.0
	maxMs := 0.0
	for _, s := range waited {
		v, ok := propOf(s, "waitMs")
		total = plus(total, orZero(v, ok))
		m, ok := propOf(s, "waitMaxMs")
		maxMs = jsMax(maxMs, orZero(m, ok))
	}
	out.Set("waited", ObjOf("sessions", float64(len(waited)), "totalMs", total, "maxMs", maxMs))
	bw, ok := propOf(week, "buildWaits")
	if !ok || bw == nil {
		bw = NewObj()
	}
	out.Set("waitsByEvent", clone(bw))
	return out
}

// BuildReport is the last closed week against the one before it.
type BuildReport struct {
	Window, Previous string
	Current, Prior   *Obj
}

// CheckBuildReport is the report card over the week rows; currentWeek is
// the ISO week still open, which is never a window.
func CheckBuildReport(weeks *Obj, currentWeek string) BuildReport {
	var closed []string
	for _, k := range weeks.Keys() {
		if jsLess(k, currentWeek) {
			closed = append(closed, k)
		}
	}
	jsSort(closed)
	var r BuildReport
	if n := len(closed); n > 0 {
		r.Window = closed[n-1]
		v, _ := weeks.Get(r.Window)
		r.Current = summarizeWeek(v)
		if n > 1 {
			r.Previous = closed[n-2]
			v, _ := weeks.Get(r.Previous)
			r.Prior = summarizeWeek(v)
		}
	}
	return r
}

const notRecorded = "not recorded"

// template is `${v}` of a property read.
func template(v any, ok bool) string {
	if !ok {
		return "undefined"
	}
	return jsString(v)
}

func secs(v any, ok bool) string {
	ms := math.NaN()
	if ok {
		ms = toNumber(v)
	}
	return jsjson.ToFixed(ms/1000, 1) + "s"
}

// RenderCheckBuildReport is the report as pull-request body lines; none
// when neither week has a figure.
func RenderCheckBuildReport(r BuildReport) []string {
	if r.Current == nil && r.Prior == nil {
		return nil
	}
	at := func(s *Obj, keys ...string) (any, bool) { return path(s, keys...) }
	tpl := func(s *Obj, keys ...string) string { return template(at(s, keys...)) }
	type row struct {
		label string
		read  func(*Obj) (string, bool)
	}
	rows := []row{
		{"Sessions reporting a build", func(s *Obj) (string, bool) { return tpl(s, "sessions"), true }},
		{"Binary cached at start", func(s *Obj) (string, bool) { return tpl(s, "builds", "cached"), true }},
		{"Compiled ok / failed / unreported", func(s *Obj) (string, bool) {
			return tpl(s, "builds", "compiledOk") + " / " + tpl(s, "builds", "compiledError") + " / " + tpl(s, "builds", "unreported"), true
		}},
		{"Compile time (n, median, max)", func(s *Obj) (string, bool) {
			if c, ok := at(s, "compiled"); !ok || !truthy(c) {
				return "", false
			}
			return tpl(s, "compiled", "count") + ", " + secs(at(s, "compiled", "medianMs")) + ", " + secs(at(s, "compiled", "maxMs")), true
		}},
		{"Sessions that waited (total, max)", func(s *Obj) (string, bool) {
			return tpl(s, "waited", "sessions") + " (" + secs(at(s, "waited", "totalMs")) + ", " + secs(at(s, "waited", "maxMs")) + ")", true
		}},
	}
	var events []string
	seen := map[string]bool{}
	for _, s := range []*Obj{r.Current, r.Prior} {
		w, _ := path(s, "waitsByEvent")
		keys, _ := entriesOf(w)
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				events = append(events, k)
			}
		}
	}
	for _, event := range jsSort(events) {
		event := event
		rows = append(rows, row{"Waits at " + event + " (n, total, max)", func(s *Obj) (string, bool) {
			w, ok := at(s, "waitsByEvent", event)
			if !ok || !truthy(w) {
				return "0", true
			}
			return template(propOf(w, "waits")) + ", " + secs(propOf(w, "totalMs")) + ", " + secs(propOf(w, "maxMs")), true
		}})
	}
	cell := func(s *Obj, read func(*Obj) (string, bool)) string {
		if s == nil {
			return notRecorded
		}
		if v, ok := read(s); ok {
			return v
		}
		return notRecorded
	}
	orNR := func(s string) string {
		if s == "" {
			return notRecorded
		}
		return s
	}
	out := []string{"### Check build", "", "| | " + orNR(r.Window) + " | " + orNR(r.Previous) + " |", "|---|---|---|"}
	for _, rw := range rows {
		out = append(out, "| "+rw.label+" | "+cell(r.Current, rw.read)+" | "+cell(r.Prior, rw.read)+" |")
	}
	return out
}
