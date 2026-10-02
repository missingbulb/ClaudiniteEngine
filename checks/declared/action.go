package declared

import (
	"fmt"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// An action declaration (scope "action", guardToolCalls) is one rule about
// a tool call judged at two moments: at PreToolUse against the call about
// to run, where a blocking finding denies it, and at Stop over every call
// the session's transcript records, where every finding advises (a call
// that ran cannot be un-run, and one the hook denied never ran).

// Call is one tool call: the tool's name and its input as parsed.
type Call struct {
	Tool  string
	Input jsjson.Value
}

// CallOf is a recorded call as a guard reads it.
func CallOf(c transcript.Call) Call { return Call{Tool: c.Name, Input: c.Input} }

// ToolCallAnchor is where a PreToolUse finding is anchored.
const ToolCallAnchor = "(tool call)"

// textAt is the value at a dot path read as text: absent or null is "", a
// string itself, anything else JSON.
func textAt(input jsjson.Value, field string) string {
	v, ok := jsjson.FieldAt(input, field)
	if !ok || v.Kind == jsjson.Null {
		return ""
	}
	return jsjson.Text(v)
}

func guardNamesTool(a map[string]any, name string) bool {
	if r := re(a["tool"]); r != nil {
		return r.Test(name)
	}
	s, _ := a["tool"].(string)
	return s == name
}

// GuardFindings judges one call against an action check's guards, with
// the session's earlier calls for atMostPerSession. A guard that cannot
// decide (a pattern past its timeout, a guard with no what or fix) is an
// error, never a finding.
func GuardFindings(c *Check, call Call, prior []Call) (out []hit, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, recovered(r)
		}
	}()
	input := call.Input
	if input.Kind == jsjson.Null {
		input = transcript.EmptyObject
	}
	whole := jsjson.Stringify(input)
	for _, a := range items(c.Spec["guardToolCalls"]) {
		if !guardNamesTool(a, call.Tool) {
			continue
		}
		field := ""
		inputField, hasField := a["inputField"].(string)
		if hasField && inputField != "" {
			field = textAt(input, inputField)
		}
		var m *Match
		if has(a, "match") {
			if m = re(a["match"]).Exec(field); m == nil {
				continue
			}
		}
		if has(a, "requireMatch") && re(a["requireMatch"]).Test(field) {
			continue
		}
		if has(a, "inputMatches") {
			mm := re(a["inputMatches"]).Exec(whole)
			if mm == nil {
				continue
			}
			if m == nil {
				m = mm
			}
		}
		if has(a, "inputFieldAbsent") {
			missing := false
			for _, k := range arr(a["inputFieldAbsent"]) {
				if _, ok := jsjson.FieldAt(input, jsString(k)); !ok {
					missing = true
				}
			}
			if !missing {
				continue
			}
		}
		if has(a, "atMostPerSession") {
			key := whole
			if inputField != "" {
				key = field
			}
			earlier := 0
			for _, p := range prior {
				if !guardNamesTool(a, p.Tool) {
					continue
				}
				pin := p.Input
				if pin.Kind == jsjson.Null {
					pin = transcript.EmptyObject
				}
				k := jsjson.Stringify(pin)
				if inputField != "" {
					k = textAt(pin, inputField)
				}
				if k == key {
					earlier++
				}
			}
			if float64(earlier) < num(a["atMostPerSession"]) {
				continue
			}
		}
		if has(a, "unlessMatches") && re(a["unlessMatches"]).Test(field) {
			continue
		}
		if has(a, "unlessInputMatches") && re(a["unlessInputMatches"]).Test(whole) {
			continue
		}
		what, okWhat := a["what"].(string)
		fix, okFix := a["fix"].(string)
		if !okWhat || !okFix {
			return nil, fmt.Errorf("a guardToolCalls entry of %s needs a what and a fix", c.ID)
		}
		vars := merge(m.groupVars(), map[string]any{"tool": call.Tool, "field": field, "match": ""})
		if m != nil {
			vars["match"] = m.Text
		}
		out = append(out, hit{What: fill(what, vars), Fix: fill(fix, vars)})
	}
	return out, nil
}

// countsCalls reports whether a guard reads the session's earlier calls.
func countsCalls(c *Check) bool {
	for _, a := range items(c.Spec["guardToolCalls"]) {
		if has(a, "atMostPerSession") {
			return true
		}
	}
	return false
}

// ActionFindings are an action check's Stop-time findings over the
// session's calls: each call judged against the calls before it, anchored
// by its tool and ordinal, every finding advisory, a call the hook denied
// marked so.
func ActionFindings(c *Check, calls []transcript.Call) ([]hit, error) {
	var out []hit
	counts := map[string]int{}
	prior := make([]Call, 0, len(calls))
	for _, tc := range calls {
		counts[tc.Name]++
		hs, err := GuardFindings(c, CallOf(tc), prior)
		if err != nil {
			return nil, err
		}
		prior = append(prior, CallOf(tc))
		denied := false
		for _, r := range tc.DeniedBy {
			denied = denied || r == c.ID
		}
		for _, h := range hs {
			h.File = fmt.Sprintf("(session) %s call #%d", tc.Name, counts[tc.Name])
			if denied {
				h.What += " (denied at the hook)"
			}
			out = append(out, h)
		}
	}
	return out, nil
}

// GuardVerdict is what the action checks say of one call about to run:
// the block lines, the advisory context lines, and the guards that could
// not decide.
type GuardVerdict struct {
	Blocks, Advice, Errors []string
}

// BuiltinRemoteBranchDelete is the engine's own guard against a Bash
// command that deletes a remote branch.
const BuiltinRemoteBranchDelete = "remote-branch-delete"

var builtinRemoteDelete = Builtin{ID: BuiltinRemoteBranchDelete, Pack: "cn", OnFail: "block", Tags: []string{"action", "pre-tool-use", "builtin"}}

var (
	deleteFlag  = mustRegex(`\bgit\s+push\b[^\n;&]*\s(--delete|-d)\s`, "")
	deleteRef   = mustRegex(`\bgit\s+push\b[^\n;&]*\s\S+\s+:\S`, "")
	remoteBlock = "never delete a remote branch — a current environment bug makes the delete-push fail, so it cannot succeed. Leave the branch; it can be deleted from the GitHub UI if needed."
)

func deletesRemoteBranch(call Call) bool {
	if call.Tool != "Bash" {
		return false
	}
	cmd, ok := call.Input.Prop("command")
	if !ok || cmd.Kind != jsjson.String {
		return false
	}
	return deleteFlag.Test(cmd.Str) || deleteRef.Test(cmd.Str)
}

// Guard judges one call about to run against the set's action checks and
// built-in guards, with the member's overrides and the grace window
// applied; a repo that is no member has the built-in guards alone. prior
// is read only when a guard counts calls.
func (s *Set) Guard(call Call, prior func() []Call, now time.Time) GuardVerdict {
	var v GuardVerdict
	var priorCalls []Call
	read := false
	render := func(h hit, why string) string {
		w := ""
		if why != "" {
			w = why + ". "
		}
		return fmt.Sprintf("%s. %sFix: %s", h.What, w, h.Fix)
	}
	for _, c := range s.Checks {
		if c.Scope != "action" || s.Config.Rules[c.ID] == "off" {
			continue
		}
		var p []Call
		if countsCalls(c) {
			if !read {
				priorCalls, read = prior(), true
			}
			p = priorCalls
		}
		hs, err := GuardFindings(c, call, p)
		if err != nil {
			v.Errors = append(v.Errors, fmt.Sprintf("the guard %s/%s could not decide: %v", c.Pack, c.ID, err))
			continue
		}
		level := c.OnFail
		if o := s.Config.Rules[c.ID]; o == "block" || o == "advise" {
			level = o
		}
		if level == "block" && c.Since != "" {
			if _, ok := graceUntil(c.Since, now); ok {
				level = "advise"
			}
		}
		for _, h := range hs {
			if level == "block" {
				v.Blocks = append(v.Blocks, fmt.Sprintf("Blocked by %s: %s", c.ID, render(h, c.Why)))
			} else {
				v.Advice = append(v.Advice, fmt.Sprintf("[claudinite %s] %s", c.ID, render(h, c.Why)))
			}
		}
	}
	if o := s.Config.Rules[BuiltinRemoteBranchDelete]; o != "off" && deletesRemoteBranch(call) {
		if o == "advise" {
			v.Advice = append(v.Advice, fmt.Sprintf("[claudinite %s] %s", BuiltinRemoteBranchDelete, remoteBlock))
		} else {
			v.Blocks = append(v.Blocks, "Blocked: "+remoteBlock)
		}
	}
	return v
}

// replyGateOpen reports whether a work check's reply-class gate lets it
// run: some reply in the session declared one of its classes. No
// transcript, no gate open.
func replyGateOpen(c *Check, session *transcript.Session) bool {
	if !has(c.Spec, "whenReplyClassIncludes") {
		return true
	}
	declared := session.ReplyClasses()
	for _, cl := range arr(c.Spec["whenReplyClassIncludes"]) {
		if s, ok := cl.(string); ok && declared[s] {
			return true
		}
	}
	return false
}

// readsSession reports whether a check asserts only with a transcript.
func readsSession(c *Check) bool {
	return c.Scope == "action" || (c.Scope == "work" && has(c.Spec, "whenReplyClassIncludes"))
}

func (s *Set) actionFinding(c *Check, h hit) findings.Finding {
	why := h.Why
	if why == "" {
		why = c.Why
	}
	return findings.Finding{Class: findings.Advisory, ID: c.ID, Pack: c.Pack, Path: h.File, Sentence: h.What, Why: why, Fix: h.Fix}
}
