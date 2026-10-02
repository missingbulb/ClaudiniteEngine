package checks

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// Call is one tool call a hook asks about: the tool, and its input and
// result as sent (nil when absent).
type Call struct {
	Tool            string
	Input, Response json.RawMessage
	Prompt          string
}

// Verdict is the guards' answer for one call: block lines, advisory
// context lines, and the guards that could not decide.
type Verdict struct {
	Blocks, Advice, Errors []string
}

// Judge judges one call at event: on PreToolUse the declared action
// checks and the built-in guards, in this process. A guard that cannot
// decide is an error line, never a block.
func (s Service) Judge(repo, event string, call Call, session *transcript.Session, deadline time.Time) Verdict {
	var v Verdict
	if event != "pre-tool-use" {
		return v
	}
	set, err := declared.LoadSet(repo, s.Build.Engine)
	if err != nil {
		set = &declared.Set{Repo: repo, Config: declared.Config{Rules: map[string]string{}}}
		v.Errors = append(v.Errors, fmt.Sprintf("the declared checks could not load: %v", err))
	}
	input := transcript.EmptyObject
	if call.Input != nil {
		if d, err := jsjson.Decode(call.Input); err == nil {
			input = d
		}
	}
	prior := func() []declared.Call {
		var out []declared.Call
		for _, c := range session.Calls() {
			out = append(out, declared.CallOf(c))
		}
		return out
	}
	g := set.Guard(declared.Call{Tool: call.Tool, Input: input}, prior, time.Now())
	v.Blocks, v.Advice = g.Blocks, g.Advice
	v.Errors = append(v.Errors, g.Errors...)
	return v
}
