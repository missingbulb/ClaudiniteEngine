package checksdk

import (
	"encoding/json"
	"time"
)

// Session is the session transcript a work run was given. A run with no
// transcript (CI, cn check world) answers every question empty.
type Session struct{ r Repo }

// Turn is one owner turn: its text and the assistant's reply up to the
// next one, with the reply's classification line and the comment classes
// it declared (correction, feature, process-change, other).
type Turn struct {
	Index     int      `json:"index"`
	Timestamp string   `json:"timestamp"`
	Text      string   `json:"text"`
	Reply     string   `json:"reply"`
	ClassLine string   `json:"classLine"`
	Classes   []string `json:"classes"`
}

// Has reports whether the reply declared class.
func (t Turn) Has(class string) bool {
	for _, c := range t.Classes {
		if c == class {
			return true
		}
	}
	return false
}

// Time is the turn's timestamp, the zero time when it has none.
func (t Turn) Time() time.Time {
	tm, _ := time.Parse(time.RFC3339Nano, t.Timestamp)
	return tm
}

// OwnerTurns are the owner's own turns, in order, from the session file
// alone.
func (s Session) OwnerTurns() []Turn {
	var out []Turn
	s.r.must("session.ownerTurns", nil, &out)
	return out
}

// ReplyClasses are every class the session's replies declared.
func (s Session) ReplyClasses() []string { return s.r.strings("session.replyClasses", nil) }

// ToolCall is one recorded tool call: DeniedBy names the rules whose
// guard denied it at the hook.
type ToolCall struct {
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	Sidechain bool            `json:"sidechain"`
	DeniedBy  []string        `json:"deniedBy"`
}

// ToolCalls are the session's tool calls in order, its subagents' included.
func (s Session) ToolCalls() []ToolCall {
	var out []ToolCall
	s.r.must("session.toolCalls", nil, &out)
	return out
}

// SkillLoads are the skills the session loaded, by every route.
func (s Session) SkillLoads() []string { return s.r.strings("session.skillLoads", nil) }
