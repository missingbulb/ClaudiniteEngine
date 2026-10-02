package declared

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

// MatchTimeout bounds one match. A pattern that backtracks past it is a
// check error, reported as a checks-run break, never a hang.
var MatchTimeout = 2 * time.Second

// CheckDeadline bounds the time one check's own work takes over a whole
// run, every match of every file it sweeps: checked before each match and
// passed into it as the match's remaining budget.
var CheckDeadline = 10 * time.Second

// budget is one check's clock over a run: the time its own work has
// taken, so one slow check never spends another's.
type budget struct {
	limit time.Duration
	used  time.Duration
	// since is when the work now running started; zero when none is.
	since time.Time
}

// left is what remains of the budget.
func (b *budget) left() time.Duration {
	l := b.limit - b.used
	if !b.since.IsZero() {
		l -= time.Since(b.since)
	}
	return l
}

// run times f against the budget.
func (b *budget) run(f func()) {
	if b == nil {
		f()
		return
	}
	b.since = time.Now()
	defer func() {
		b.used += time.Since(b.since)
		b.since = time.Time{}
	}()
	f()
}

func (b *budget) exceeded() timeoutError {
	return timeoutError{fmt.Errorf("ran past its %v deadline", b.limit)}
}

// bind sets b as the budget of every pattern in v, a check's spec or a
// part of it; nil clears it.
func bind(v any, b *budget) {
	switch x := v.(type) {
	case *Regex:
		if x != nil {
			x.budget = b
		}
	case map[string]any:
		for _, e := range x {
			bind(e, b)
		}
	case []any:
		for _, e := range x {
			bind(e, b)
		}
	case []map[string]any:
		for _, e := range x {
			bind(e, b)
		}
	}
}

// Regex is one compiled pattern, matched with ECMAScript semantics so a
// declaration's lookarounds and backreferences run as written.
type Regex struct {
	re     *regexp2.Regexp
	Source string
	Flags  string
	names  []string
	// budget is the clock of the check the pattern belongs to while it
	// runs; nil outside a run, where only MatchTimeout bounds a match.
	budget *budget
}

// Match is one match: its rune offset, its text and its named groups.
type Match struct {
	Index int
	Text  string
	// Groups holds every named group; an unset one is absent from Groups
	// and listed in Unset.
	Groups map[string]string
	Unset  []string
}

// timeoutError is raised through a panic from deep inside a family and
// recovered per check, so a runaway pattern fails that check alone.
type timeoutError struct{ err error }

func (t timeoutError) Error() string { return t.err.Error() }

// compileRegex compiles body with flags drawn from m, i and s.
func compileRegex(body, flags string) (*Regex, error) {
	opt := regexp2.RegexOptions(regexp2.ECMAScript)
	for _, f := range flags {
		switch f {
		case 'i':
			opt |= regexp2.IgnoreCase
		case 'm':
			opt |= regexp2.Multiline
		case 's':
			opt |= regexp2.Singleline
		default:
			return nil, fmt.Errorf("the flag %q is not one this engine takes (m, i and s are)", f)
		}
	}
	if strings.Contains(flags, "s") {
		// ECMAScript mode refuses Singleline; the dot then matches every
		// character, which a class says the same way.
		opt &^= regexp2.Singleline
		body = dotAll(body)
	}
	re, err := regexp2.Compile(body, opt)
	if err != nil {
		return nil, err
	}
	r := &Regex{re: re, Source: body, Flags: flags}
	for _, n := range re.GetGroupNames() {
		if !isDigits(n) {
			r.names = append(r.names, n)
		}
	}
	return r, nil
}

// mustRegex compiles a pattern the engine itself writes.
func mustRegex(body, flags string) *Regex {
	r, err := compileRegex(body, flags)
	if err != nil {
		panic(err)
	}
	return r
}

// dotAll rewrites each unescaped dot outside a class as [\s\S].
func dotAll(body string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c == '\\' && i+1 < len(body):
			b.WriteByte(c)
			i++
			b.WriteByte(body[i])
			continue
		case c == '[' && !inClass:
			inClass = true
		case c == ']' && inClass:
			inClass = false
		case c == '.' && !inClass:
			b.WriteString(`[\s\S]`)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// arm gives the next match its timeout: MatchTimeout, or what is left of
// the check's budget when that is less; a check already past its
// deadline stops here.
func (r *Regex) arm() {
	t := MatchTimeout
	if r.budget != nil {
		left := r.budget.left()
		if left <= 0 {
			panic(r.budget.exceeded())
		}
		if left < t {
			t = left
		}
	}
	if r.re.MatchTimeout != t {
		r.re.MatchTimeout = t
	}
}

// failed is the panic for a match that returned err.
func (r *Regex) failed(err error) {
	if r.budget != nil && r.budget.left() <= 0 {
		panic(r.budget.exceeded())
	}
	panic(timeoutError{fmt.Errorf("the pattern /%s/%s: %w", r.Source, r.Flags, err)})
}

// Test reports whether s holds a match.
func (r *Regex) Test(s string) bool {
	if r == nil {
		return false
	}
	r.arm()
	ok, err := r.re.MatchString(s)
	if err != nil {
		r.failed(err)
	}
	return ok
}

// Exec returns the first match in s, or nil.
func (r *Regex) Exec(s string) *Match {
	return r.execAt(s, 0)
}

func (r *Regex) execAt(s string, start int) *Match {
	if r == nil {
		return nil
	}
	r.arm()
	m, err := r.re.FindStringMatchStartingAt(s, start)
	if err != nil {
		r.failed(err)
	}
	return r.wrap(m)
}

func (r *Regex) wrap(m *regexp2.Match) *Match {
	if m == nil {
		return nil
	}
	out := &Match{Index: m.Index, Text: m.String(), Groups: map[string]string{}}
	for _, n := range r.names {
		g := m.GroupByName(n)
		if g == nil || len(g.Captures) == 0 {
			out.Unset = append(out.Unset, n)
			continue
		}
		out.Groups[n] = g.String()
	}
	return out
}

// HasGroups reports whether the pattern names any group.
func (r *Regex) HasGroups() bool { return len(r.names) > 0 }

// groupVars are a match's named groups as template variables; an unset
// group interpolates as JavaScript prints it.
func (m *Match) groupVars() map[string]any {
	out := map[string]any{}
	if m == nil {
		return out
	}
	for k, v := range m.Groups {
		out[k] = v
	}
	for _, k := range m.Unset {
		out[k] = undefined
	}
	return out
}

// lineOf is the 1-based line the rune offset idx falls on in text.
func lineOf(text string, idx int) int {
	line := 1
	n := 0
	for _, c := range text {
		if n >= idx {
			break
		}
		if c == '\n' {
			line++
		}
		n++
	}
	return line
}

// split splits s at each match of r as String.prototype.split does,
// capture groups included.
func (r *Regex) split(s string) []string {
	runes := []rune(s)
	var out []string
	last, from := 0, 0
	for from <= len(runes) {
		r.arm()
		m, err := r.re.FindRunesMatchStartingAt(runes, from)
		if err != nil {
			r.failed(err)
		}
		if m == nil || m.Index >= len(runes) {
			break
		}
		end := m.Index + m.Length
		if end == last || (m.Length == 0 && m.Index == last) {
			from = m.Index + 1
			continue
		}
		out = append(out, string(runes[last:m.Index]))
		for _, g := range m.Groups()[1:] {
			out = append(out, g.String())
		}
		last = end
		from = end
		if m.Length == 0 {
			from++
		}
	}
	return append(out, string(runes[last:]))
}

// validUTF8 reads a file's bytes as Node's utf8 decoding does.
func validUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), "�")
}
