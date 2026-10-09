package usage

import (
	"math"
	"regexp"
	"strconv"
	"time"
)

// isoDateRE is the date-time string format Date.parse reads, the one the
// fold's every instant is written in: a date, optionally a time to the
// minute, second or fraction, optionally an offset.
var isoDateRE = regexp.MustCompile(`^([+-]\d{6}|\d{4})(?:-(\d{2})(?:-(\d{2}))?)?(?:[Tt](\d{2}):(\d{2})(?::(\d{2})(?:\.(\d+))?)?([Zz]|[+-]\d{2}:\d{2})?)?$`)

const maxTimeMs = 8.64e15

// parseDate is Date.parse(s) for the ISO format, NaN for anything else. A
// date alone is UTC; a time without an offset is the runner's zone, which
// is UTC. The engine's legacy fallbacks for other spellings are not read.
func parseDate(s string) float64 {
	m := isoDateRE.FindStringSubmatch(s)
	if m == nil || m[1] == "-000000" {
		return math.NaN()
	}
	atoi := func(x string, dflt int) int {
		if x == "" {
			return dflt
		}
		n, _ := strconv.Atoi(x)
		return n
	}
	year := atoi(m[1], 0)
	month, day := atoi(m[2], 1), atoi(m[3], 1)
	hour, minute, second := atoi(m[4], 0), atoi(m[5], 0), atoi(m[6], 0)
	ms := 0
	if m[7] != "" {
		frac := (m[7] + "00")[:3]
		ms = atoi(frac, 0)
	}
	switch {
	case month < 1 || month > 12, day < 1 || day > 31,
		hour > 24, minute > 59, second > 59,
		hour == 24 && (minute != 0 || second != 0 || ms != 0):
		return math.NaN()
	}
	offset := 0
	if z := m[8]; z != "" && z != "Z" && z != "z" {
		oh, om := atoi(z[1:3], 0), atoi(z[4:6], 0)
		if oh > 23 || om > 59 {
			return math.NaN()
		}
		offset = (oh*60 + om) * 60000
		if z[0] == '-' {
			offset = -offset
		}
	}
	days := daysFromCivil(year, month, 1) + int64(day-1)
	t := float64(days)*86400000 + float64(hour*3600000+minute*60000+second*1000+ms) - float64(offset)
	if math.Abs(t) > maxTimeMs {
		return math.NaN()
	}
	return t
}

// daysFromCivil is the days from 1970-01-01 to y-m-d, proleptic Gregorian.
func daysFromCivil(y, m, d int) int64 {
	if m <= 2 {
		y--
	}
	era := y
	if era < 0 {
		era -= 399
	}
	era /= 400
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return int64(era)*146097 + int64(doe) - 719468
}

// isoString is new Date(ms).toISOString() for a finite instant in range.
func isoString(ms float64) string {
	t := time.UnixMilli(int64(ms)).UTC()
	y := t.Year()
	var year string
	switch {
	case y >= 0 && y <= 9999:
		year = pad(y, 4)
	case y < 0:
		year = "-" + pad(-y, 6)
	default:
		year = "+" + pad(y, 6)
	}
	return year + t.Format("-01-02T15:04:05.000Z")
}

func pad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// utcDay is a Date's getUTCDay(), Sunday 0.
func utcDay(ms float64) int {
	return int(time.UnixMilli(int64(ms)).UTC().Weekday())
}

// utcYear is a Date's getUTCFullYear().
func utcYear(ms float64) int {
	return time.UnixMilli(int64(ms)).UTC().Year()
}
