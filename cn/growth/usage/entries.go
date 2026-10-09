package usage

// The transcript entry shapes every counter reads, in one place: a human
// turn, a typed slash command, a Skill load, a tool call and an entry's
// every string.

// IsUserMessage is a genuine human turn: a user entry stamped with the
// human origin. Everything else a user-role entry can be lacks it.
func IsUserMessage(entry any) bool {
	t, _ := pathString(entry, "type")
	k, _ := pathString(entry, "origin", "kind")
	return t == "user" && k == "human"
}

var commandRE = jsPattern(`<command-name>\s*\/?([A-Za-z0-9:_-]+)\s*<\/command-name>`)

// CommandName is the bare name of a user-typed slash command, false for
// any other entry.
func CommandName(entry any) (string, bool) {
	if t, _ := pathString(entry, "type"); t != "user" {
		return "", false
	}
	content, ok := pathString(entry, "message", "content")
	if !ok {
		return "", false
	}
	m := commandRE.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func contentBlocks(entry any) ([]any, bool) {
	c, _ := path(entry, "message", "content")
	arr, ok := c.([]any)
	return arr, ok
}

// SkillToolLoads are the skills an assistant entry's Skill tool calls
// load.
func SkillToolLoads(entry any) []string {
	if t, _ := pathString(entry, "type"); t != "assistant" {
		return nil
	}
	blocks, ok := contentBlocks(entry)
	if !ok {
		return nil
	}
	var out []string
	for _, b := range blocks {
		typ, _ := pathString(b, "type")
		name, _ := pathString(b, "name")
		skill, isStr := pathString(b, "input", "skill")
		if typ == "tool_use" && name == "Skill" && isStr {
			out = append(out, skill)
		}
	}
	return out
}

// ToolCall is one tool_use block.
type ToolCall struct {
	Name  string
	Input any
	ID    any
}

// ToolCalls are an assistant entry's tool_use blocks.
func ToolCalls(entry any) []ToolCall {
	if t, _ := pathString(entry, "type"); t != "assistant" {
		return nil
	}
	blocks, ok := contentBlocks(entry)
	if !ok {
		return nil
	}
	var out []ToolCall
	for _, b := range blocks {
		typ, _ := pathString(b, "type")
		name, isStr := pathString(b, "name")
		if typ != "tool_use" || !isStr {
			continue
		}
		input, has := path(b, "input")
		if !has || input == nil {
			input = NewObj()
		}
		id, _ := path(b, "id")
		out = append(out, ToolCall{Name: name, Input: input, ID: id})
	}
	return out
}

// EntryText is every string anywhere in a value, in order.
func EntryText(v any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case *Obj:
			for _, k := range x.Keys() {
				e, _ := x.Get(k)
				walk(e)
			}
		}
	}
	walk(v)
	return out
}
