package checks

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/run"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
)

// Call is one call a hook asks about: the tool, its input and result as
// sent (nil when absent), or the prompt.
type Call = run.Call

// Verdict is the guards' answer for one call: block lines, advisory
// context lines, and the guards that could not decide.
type Verdict struct {
	Blocks, Advice, Errors []string
}

// Judge judges one call at event: on PreToolUse the declared action
// checks and the built-in guards, in this process; then, at any event the
// judges manifest names, the packs' coded judges in the checks binary,
// within what is left before deadline. A guard or judge that cannot
// decide is an error line, never a block.
func (s Service) Judge(repo, event string, call Call, session *transcript.Session, deadline time.Time) Verdict {
	var v Verdict
	var set *declared.Set
	load := func() *declared.Set {
		if set == nil {
			var err error
			if set, err = s.LoadSet(repo); err != nil {
				set = &declared.Set{Repo: repo, Config: declared.Config{Rules: map[string]string{}}}
				v.Errors = append(v.Errors, fmt.Sprintf("the declared checks could not load: %v", err))
			}
		}
		return set
	}
	if event == "pre-tool-use" {
		s.guard(load(), call, session, &v)
	}
	s.coded(repo, event, call, func() declared.Config { return load().Config }, deadline, &v)
	return v
}

func (s Service) guard(set *declared.Set, call Call, session *transcript.Session, v *Verdict) {
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
}

// coded runs the coded judges for event when the built binary's manifest
// names one: a finding blocks on PreToolUse and advises elsewhere, an
// advisory always advises, the member's rules (read only then) override
// either.
func (s Service) coded(repo, event string, call Call, config func() declared.Config, deadline time.Time, v *Verdict) {
	key, _, err := s.Key(repo)
	if err != nil {
		v.Errors = append(v.Errors, "the coded judges could not run: "+err.Error())
		return
	}
	if key == "" {
		return
	}
	binary, err := build.Wait(s.Build, key, 0)
	if err != nil {
		v.Advice = append(v.Advice, s.notBuilt(key))
		return
	}
	judges, err := build.Judges(s.Build, key)
	if err != nil {
		v.Errors = append(v.Errors, "the coded judges could not run: "+err.Error())
		return
	}
	if len(judges[event]) == 0 {
		return
	}
	left := time.Until(deadline)
	if left <= 0 {
		v.Errors = append(v.Errors, "the coded judges did not run: the hook's deadline has passed")
		return
	}
	res := run.Runner{Binary: binary, Engine: s.Build.Engine, Silence: left}.Judge(event, call, repo)
	if res.Err != nil {
		v.Errors = append(v.Errors, "the coded judges could not judge: "+res.Err.Error())
		return
	}
	v.Errors = append(v.Errors, res.Errors...)
	if len(res.Findings) == 0 {
		return
	}
	cfg := config()
	for _, f := range res.Findings {
		pack, id := splitName(f.Check)
		level := "advise"
		if f.Class != "advisory" && event == "pre-tool-use" {
			level = "block"
		}
		switch cfg.Rule(pack, id) {
		case "off":
			continue
		case "advise":
			level = "advise"
		case "block":
			if event == "pre-tool-use" {
				level = "block"
			}
		}
		if level == "block" {
			v.Blocks = append(v.Blocks, fmt.Sprintf("Blocked by %s: %s", id, f.Sentence))
		} else {
			v.Advice = append(v.Advice, fmt.Sprintf("[claudinite %s] %s", id, f.Sentence))
		}
	}
}

// notBuilt is the advice of a judge that found no binary for key: the
// coded judges did not run, which the session must see on every call
// until they do.
func (s Service) notBuilt(key string) string {
	if build.Failed(s.Build, key) {
		return "[claudinite] the coded checks did not run: the checks build failed; fix the check source it names in " + filepath.Join(s.Build.Dir(key), "build.log")
	}
	return "[claudinite] the coded checks did not run: the checks binary was not built in time"
}
