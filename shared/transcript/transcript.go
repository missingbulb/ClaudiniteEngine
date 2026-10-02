// Package transcript reads a Claude Code session transcript as the Node
// engine's session-transcript.mjs reads it: JSONL, one entry a line, a
// partial trailing line skipped. An owner turn is a `type: "user"` entry,
// neither meta nor sidechain, whose content is a string or all text blocks
// and does not start with "<" (the harness's pseudo-turns). What the
// session did — its tool calls and skill loads — is read across the session
// file and each subagent's own stream beside it,
// <dir>/<id>/subagents/agent-*.jsonl; owner turns from the session file
// alone, since a subagent's prompt is a user entry too.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
)

// maxLine bounds one transcript line.
const maxLine = 64 << 20

// Entry is one transcript line.
type Entry struct {
	Type        string
	IsMeta      bool
	IsSidechain bool
	Timestamp   string
	// Text is the content when it is a string; Blocks when it is a list.
	Text     string
	IsString bool
	Blocks   []Block
}

// Block is one content block.
type Block struct {
	Type string
	// Text is the block's text when it is a string.
	Text    string
	HasText bool
	Name    string
	HasName bool
	ID      string
	Input   jsjson.Value
	// HasInput is false when input is absent or null.
	HasInput  bool
	ToolUseID string
	IsError   bool
	// Content is a tool result's content as text.
	Content string
}

type rawEntry struct {
	Type        any `json:"type"`
	IsMeta      any `json:"isMeta"`
	IsSidechain any `json:"isSidechain"`
	Timestamp   any `json:"timestamp"`
	Message     *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type rawBlock struct {
	Type      any             `json:"type"`
	Text      any             `json:"text"`
	Name      any             `json:"name"`
	ID        any             `json:"id"`
	Input     json.RawMessage `json:"input"`
	ToolUseID any             `json:"tool_use_id"`
	IsError   any             `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// Parse reads JSONL text into entries; a line that is not a JSON object is
// skipped.
func Parse(data []byte) []Entry {
	return parseLines(data, nil)
}

// parseLines is Parse over the lines keep accepts (every line when nil).
func parseLines(data []byte, keep func([]byte) bool) []Entry {
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		l := sc.Bytes()
		if len(bytes.TrimSpace(l)) == 0 || keep != nil && !keep(l) {
			continue
		}
		var r rawEntry
		if json.Unmarshal(l, &r) != nil {
			continue
		}
		e := Entry{IsMeta: truthy(r.IsMeta), IsSidechain: truthy(r.IsSidechain)}
		e.Type, _ = str(r.Type)
		e.Timestamp, _ = str(r.Timestamp)
		if r.Message != nil && len(r.Message.Content) > 0 {
			var s string
			var blocks []json.RawMessage
			switch {
			case json.Unmarshal(r.Message.Content, &s) == nil:
				e.Text, e.IsString = s, true
			case json.Unmarshal(r.Message.Content, &blocks) == nil:
				e.Blocks = []Block{}
				for _, b := range blocks {
					e.Blocks = append(e.Blocks, block(b))
				}
			}
		}
		out = append(out, e)
	}
	return out
}

func block(raw json.RawMessage) Block {
	var r rawBlock
	if json.Unmarshal(raw, &r) != nil {
		return Block{}
	}
	b := Block{IsError: truthy(r.IsError)}
	b.Type, _ = str(r.Type)
	b.Text, b.HasText = str(r.Text)
	b.Name, b.HasName = str(r.Name)
	b.ID, _ = str(r.ID)
	b.ToolUseID, _ = str(r.ToolUseID)
	if len(r.Input) > 0 && string(r.Input) != "null" {
		if v, err := jsjson.Decode(r.Input); err == nil {
			b.Input, b.HasInput = v, true
		}
	}
	if len(r.Content) > 0 {
		var s string
		var parts []rawBlock
		switch {
		case json.Unmarshal(r.Content, &s) == nil:
			b.Content = s
		case json.Unmarshal(r.Content, &parts) == nil:
			texts := make([]string, len(parts))
			for i, p := range parts {
				texts[i], _ = str(p.Text)
			}
			b.Content = strings.Join(texts, "\n")
		}
	}
	return b
}

// Entries reads one transcript file; an absent or unreadable one has none.
func Entries(path string) []Entry {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return Parse(data)
}

// Paths are every file one session writes: the session file when it
// exists, then <dir>/<id>/subagents/agent-*.jsonl, sorted.
func Paths(sessionFile string) []string {
	if sessionFile == "" {
		return nil
	}
	var out []string
	if _, err := os.Stat(sessionFile); err == nil {
		out = append(out, sessionFile)
	}
	sub := filepath.Join(filepath.Dir(sessionFile), strings.TrimSuffix(filepath.Base(sessionFile), ".jsonl"), "subagents")
	ents, err := os.ReadDir(sub)
	if err != nil {
		return out
	}
	var names []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "agent-") && strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, filepath.Join(sub, n))
	}
	return out
}

// SessionEntries are every entry the session's files hold, in order.
func SessionEntries(paths []string) []Entry {
	var out []Entry
	for _, p := range paths {
		out = append(out, Entries(p)...)
	}
	return out
}

func humanText(e Entry) (string, bool) {
	if e.Type != "user" || e.IsMeta || e.IsSidechain {
		return "", false
	}
	var text string
	switch {
	case e.IsString:
		text = e.Text
	case len(e.Blocks) > 0:
		parts := make([]string, len(e.Blocks))
		for i, b := range e.Blocks {
			if b.Type != "text" {
				return "", false
			}
			parts[i] = b.Text
		}
		text = strings.Join(parts, "\n")
	default:
		return "", false
	}
	if text == "" || strings.HasPrefix(strings.TrimLeftFunc(text, jsSpace), "<") {
		return "", false
	}
	return text, true
}

func jsSpace(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\v', r == '\f', r == '\r', r == ' ', r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}

// Turn is one owner turn.
type Turn struct {
	Index     int
	Timestamp string
	Text      string
	// Classes are the comment classes the reply after it declared.
	Classes map[string]bool
}

// OwnerTurns are the owner's own turns, in order.
func OwnerTurns(es []Entry) []Turn {
	var out []Turn
	for i, e := range es {
		if t, ok := humanText(e); ok {
			out = append(out, Turn{Index: i, Timestamp: e.Timestamp, Text: t})
		}
	}
	return out
}

// AssistantTextAfter is the assistant text after entry from, up to the
// next owner turn.
func AssistantTextAfter(es []Entry, from int) string {
	var parts []string
	for i := from + 1; i < len(es); i++ {
		if _, ok := humanText(es[i]); ok {
			break
		}
		if es[i].Type != "assistant" {
			continue
		}
		for _, b := range es[i].Blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

const lineEnd = `\n\r\x{2028}\x{2029}`

// ws is JavaScript's \s.
const ws = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	classLine  = regexp.MustCompile(`(?i)(?:^|[` + lineEnd + `])([ \t>#*_-]*comment` + ws + `+class\b[^:\n]*:[^` + lineEnd + `]*)`)
	classToken = regexp.MustCompile(`(?i)correction|feature|process(?:` + ws + `|-)change|other`)
	spaces     = regexp.MustCompile(ws + `+`)
)

// ClassificationLine is the reply's explicit classification line, or "".
func ClassificationLine(text string) string {
	m := classLine.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// ClassesIn are the canonical classes a classification line names:
// correction, feature, process-change, other.
func ClassesIn(line string) map[string]bool {
	out := map[string]bool{}
	for _, m := range classToken.FindAllString(line, -1) {
		out[spaces.ReplaceAllString(strings.ToLower(m), "-")] = true
	}
	return out
}

// ClassifiedTurns are the owner turns with the classes each reply declared.
func ClassifiedTurns(es []Entry) []Turn {
	turns := OwnerTurns(es)
	for i := range turns {
		turns[i].Classes = ClassesIn(ClassificationLine(AssistantTextAfter(es, turns[i].Index)))
	}
	return turns
}

// ReplyClasses are every class the session's replies declared.
func ReplyClasses(es []Entry) map[string]bool {
	out := map[string]bool{}
	for _, t := range ClassifiedTurns(es) {
		for c := range t.Classes {
			out[c] = true
		}
	}
	return out
}

// Call is one recorded tool call.
type Call struct {
	Index     int
	Name      string
	Input     jsjson.Value
	Sidechain bool
	// DeniedBy are the rules whose `Blocked by <rule>:` lines the call's
	// error result carries: the PreToolUse guard denied it.
	DeniedBy []string
}

// InputJSON is the input as JSON.stringify writes it.
func (c Call) InputJSON() string { return jsjson.Stringify(c.Input) }

var denial = regexp.MustCompile(`(?:^|\n)Blocked by ([\w.-]+):`)

func denials(es []Entry) map[string][]string {
	out := map[string][]string{}
	for _, e := range es {
		if e.Type != "user" {
			continue
		}
		for _, b := range e.Blocks {
			if b.Type != "tool_result" || !b.IsError || b.ToolUseID == "" {
				continue
			}
			var rules []string
			for _, m := range denial.FindAllStringSubmatch(b.Content, -1) {
				rules = append(rules, m[1])
			}
			if len(rules) > 0 {
				out[b.ToolUseID] = rules
			}
		}
	}
	return out
}

// EmptyObject is {}.
var EmptyObject = jsjson.Value{Kind: jsjson.Object, Obj: map[string]jsjson.Value{}}

// ToolCalls are every tool_use block on an assistant entry, in order.
func ToolCalls(es []Entry) []Call {
	denied := denials(es)
	var out []Call
	for i, e := range es {
		if e.Type != "assistant" {
			continue
		}
		for _, b := range e.Blocks {
			if b.Type != "tool_use" || !b.HasName {
				continue
			}
			in := b.Input
			if !b.HasInput {
				in = EmptyObject
			}
			out = append(out, Call{Index: i, Name: b.Name, Input: in, Sidechain: e.IsSidechain, DeniedBy: denied[b.ID]})
		}
	}
	return out
}

var (
	skillFile   = regexp.MustCompile(`(?:^|/)skills/([^/]+)/SKILL\.md$`)
	commandName = regexp.MustCompile(`<command-name>\s*/?([A-Za-z0-9:_-]+)\s*</command-name>`)
)

// SkillLoads are the skills loaded, in order: a Skill tool call's
// input.skill, a Read of a skills/<name>/SKILL.md, a <command-name> block
// in a user entry (the slash dropped, a plugin: prefix kept).
func SkillLoads(es []Entry) []string {
	var out []string
	for _, e := range es {
		if e.Type == "user" {
			text := e.Text
			if !e.IsString {
				parts := make([]string, len(e.Blocks))
				for i, b := range e.Blocks {
					parts[i] = b.Text
				}
				text = strings.Join(parts, "\n")
			}
			for _, m := range commandName.FindAllStringSubmatch(text, -1) {
				out = append(out, m[1])
			}
			continue
		}
		if e.Type != "assistant" {
			continue
		}
		for _, b := range e.Blocks {
			if b.Type != "tool_use" {
				continue
			}
			if b.Name == "Skill" {
				if s, ok := b.Input.Prop("skill"); ok && s.Kind == jsjson.String && b.Input.Kind == jsjson.Object {
					out = append(out, s.Str)
				}
			}
			if b.Name == "Read" && b.Input.Kind == jsjson.Object {
				if p, ok := b.Input.Prop("file_path"); ok && p.Kind == jsjson.String {
					if m := skillFile.FindStringSubmatch(p.Str); m != nil {
						out = append(out, m[1])
					}
				}
			}
		}
	}
	return out
}

// mayLoad reports whether a raw line can name a skill load: every route
// needs one of these words in the decoded text, and a raw line holds the
// word itself unless a \u escape spells one of its letters.
func mayLoad(l []byte) bool {
	return bytes.Contains(l, []byte("Skill")) || bytes.Contains(l, []byte("SKILL.md")) ||
		bytes.Contains(l, []byte("command-name")) || bytes.Contains(l, []byte(`\u`))
}

// loadsIn is SkillLoads(Parse(data)), parsing only the lines that can
// name a load.
func loadsIn(data []byte) []string { return SkillLoads(parseLines(data, mayLoad)) }

// Session is one session's transcript, read at most once and only when a
// verdict asks for it. A nil Session, or one with no path, has nothing.
type Session struct {
	Path string

	loadOnce sync.Once
	loaded   map[string]bool
	once     sync.Once
	all      []Entry
	own      []Entry
	calls    []Call
}

// NewSession is the session whose transcript is at path; "" is none.
func NewSession(path string) *Session {
	if path == "" {
		return nil
	}
	return &Session{Path: path}
}

// read parses every file of the session, the session file once.
func (s *Session) read() {
	s.once.Do(func() {
		for _, p := range Paths(s.Path) {
			es := Entries(p)
			if p == s.Path {
				s.own = es
			}
			s.all = append(s.all, es...)
		}
		s.calls = ToolCalls(s.all)
	})
}

// readLoads finds the session's loads from the full read when there was
// one, else parsing only the lines that can name one: a hold or a nudge
// asks for nothing more.
func (s *Session) readLoads() {
	s.loadOnce.Do(func() {
		s.loaded = map[string]bool{}
		if s.all != nil {
			for _, n := range SkillLoads(s.all) {
				s.loaded[n] = true
			}
			return
		}
		for _, p := range Paths(s.Path) {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			for _, n := range loadsIn(data) {
				s.loaded[n] = true
			}
		}
	})
}

// Present reports whether the session names a transcript.
func (s *Session) Present() bool { return s != nil && s.Path != "" }

// Loaded is the set of skills the session loaded.
func (s *Session) Loaded() map[string]bool {
	if !s.Present() {
		return map[string]bool{}
	}
	s.readLoads()
	return s.loaded
}

// Calls are the session's tool calls.
func (s *Session) Calls() []Call {
	if !s.Present() {
		return nil
	}
	s.read()
	return s.calls
}

// ReplyClasses are the classes the session's replies declared, read from
// the session file alone.
func (s *Session) ReplyClasses() map[string]bool {
	if !s.Present() {
		return map[string]bool{}
	}
	s.read()
	return ReplyClasses(s.own)
}

// Read reports whether the transcript has been read, for the tests that pin
// its laziness.
func (s *Session) Read() bool {
	if !s.Present() {
		return false
	}
	return s.loaded != nil || s.all != nil || s.own != nil
}
