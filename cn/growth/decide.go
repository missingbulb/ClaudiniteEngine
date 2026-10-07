package growth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/growth/capture"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/prune"
	sharedgrowth "github.com/missingbulb/ClaudiniteEngine/cn/shared/growth"
)

// Cores are the decisions Decide answers.
var Cores = []string{"parselines", "bundle", "slice", "redactions", "scrub", "logname", "parsename", "findtranscript", "prune", "retention"}

// Decide answers one of the capture's and the prune's decision cores over
// a fixture's input: the parity harness's growth face, never a run.
func Decide(core string, raw []byte) (any, error) {
	var in struct {
		Text        string                         `json:"text"`
		Streams     []string                       `json:"streams"`
		LastTs      *string                        `json:"lastTs"`
		Env         json.RawMessage                `json:"env"`
		Extra       []struct{ Name, Value string } `json:"extra"`
		Credentials json.RawMessage                `json:"credentials"`
		Redactions  []capture.Redaction            `json:"redactions"`
		Now         string                         `json:"now"`
		PR          *int                           `json:"pr"`
		Issue       *int                           `json:"issue"`
		Session     string                         `json:"session"`
		Names       []string                       `json:"names"`
		Root        string                         `json:"root"`
		SessionID   string                         `json:"sessionId"`
		Files       map[string]struct {
			Mtime   string  `json:"mtime"`
			Content *string `json:"content"`
		} `json:"files"`
		RetentionDays *float64 `json:"retentionDays"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	streams := func() []capture.Bundled {
		var s [][]capture.Line
		for _, t := range in.Streams {
			s = append(s, capture.ParseLines(t))
		}
		return capture.Bundle(s)
	}
	switch core {
	case "parselines":
		out := []map[string]any{}
		for _, l := range capture.ParseLines(in.Text) {
			out = append(out, map[string]any{"raw": l.Raw, "entry": l.Value})
		}
		return out, nil
	case "bundle":
		out := []map[string]any{}
		for _, b := range streams() {
			out = append(out, map[string]any{"raw": b.Raw, "ts": b.TS})
		}
		return out, nil
	case "slice":
		b := streams()
		last := ""
		if in.LastTs != nil {
			last = *in.LastTs
		}
		delta := []string{}
		for _, l := range capture.SliceAfter(b, last) {
			delta = append(delta, l.Raw)
		}
		var max any
		if m := capture.MaxTimestamp(b); m != "" {
			max = m
		}
		return map[string]any{"delta": delta, "max": max}, nil
	case "redactions":
		var env []string
		if len(in.Env) > 0 {
			var err error
			if env, err = capture.EnvFromJSON(in.Env); err != nil {
				return nil, err
			}
		}
		var extra []capture.Value
		for _, e := range in.Extra {
			extra = append(extra, capture.Value{Name: e.Name, Value: e.Value})
		}
		if len(in.Credentials) > 0 {
			extra = append(extra, capture.CredentialValues(in.Credentials)...)
		}
		out := capture.Redactions(env, extra)
		if out == nil {
			out = []capture.Redaction{}
		}
		return out, nil
	case "scrub":
		return capture.Scrub(in.Text, in.Redactions), nil
	case "logname":
		now, err := time.Parse(time.RFC3339Nano, in.Now)
		if err != nil {
			return nil, err
		}
		return capture.LogFilename(now, capture.Key{PR: in.PR, Issue: in.Issue}, in.Session), nil
	case "parsename":
		out := []any{}
		for _, n := range in.Names {
			if p, ok := capture.ParseLogFilename(n); ok {
				out = append(out, p)
			} else {
				out = append(out, nil)
			}
		}
		return out, nil
	case "findtranscript":
		projects, err := os.MkdirTemp("", "cn-growth-projects-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(projects) }()
		for rel, f := range in.Files {
			p := filepath.Join(projects, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return nil, err
			}
			body := "{}\n"
			if f.Content != nil {
				body = *f.Content
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				return nil, err
			}
			at, err := time.Parse(time.RFC3339Nano, f.Mtime)
			if err != nil {
				return nil, err
			}
			if err := os.Chtimes(p, at, at); err != nil {
				return nil, err
			}
		}
		hit := capture.FindTranscript(in.Root, in.SessionID, projects)
		if hit == "" {
			return nil, nil
		}
		rel, err := filepath.Rel(projects, hit)
		return filepath.ToSlash(rel), err
	case "prune":
		now, err := time.Parse(time.RFC3339Nano, in.Now)
		if err != nil {
			return nil, err
		}
		return prune.PlanPrune(in.Names, in.RetentionDays, now), nil
	case "retention":
		var probe map[string]any
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, err
		}
		v, present := probe["declared"]
		return map[string]any{"days": sharedgrowth.ResolveRetentionDays(v, present)}, nil
	}
	return nil, fmt.Errorf("unknown growth core %q: one of %v", core, Cores)
}
