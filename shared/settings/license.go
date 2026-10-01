package settings

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The license block sits beside engine and names the plan the repo is on,
// which selects the key endpoint a session asks (the public one for
// "public", the paid one otherwise). It never grants anything: the server
// issues what its own tables say. cn init writes it and the nightly update
// corrects it by a one-line PR.
//
//	license:                     [license]                      "license": {"plan": "public"}
//	  plan: "public"             plan = "public"
//
// An absent block, or a block without plan, means the paid endpoint.

// PlanPattern is a plan a settings file may name.
var PlanPattern = regexp.MustCompile(`^(public|private-repo|personal|organization|internal)$`)

// License is the license block.
type License struct {
	Plan string
	// Present is false when the file holds no license block.
	Present bool
}

func licensePlan(raw []byte, f Format) (span, bool, bool, error) {
	spans, present, err := namedBlockSpans(raw, f, licensePatterns, false)
	if err != nil || !present {
		return span{}, false, present, err
	}
	s, ok, err := keyIn(raw, f, spans, "license", "plan")
	return s, ok, true, err
}

// ReadLicense reads and validates the license block.
func ReadLicense(raw []byte, f Format) (License, error) {
	s, ok, present, err := licensePlan(raw, f)
	if err != nil {
		return License{}, err
	}
	l := License{Present: present}
	if ok {
		l.Plan = string(raw[s.start:s.end])
		if !PlanPattern.MatchString(l.Plan) {
			return License{}, fmt.Errorf("license.plan must be one of public, private-repo, personal, organization or internal, not %q", l.Plan)
		}
	}
	return l, nil
}

// SetPlan writes license.plan: the value in place when the key is there,
// the key at the top of the block when only the block is, else the block
// after the engine block (YAML, JSON) or at the end (TOML). No other byte
// changes.
func SetPlan(raw []byte, f Format, plan string) ([]byte, error) {
	if !PlanPattern.MatchString(plan) {
		return nil, fmt.Errorf("refusing to write license.plan %q: not a plan", plan)
	}
	if _, err := ReadEngine(raw, f); err != nil {
		return nil, err
	}
	if _, err := ReadLicense(raw, f); err != nil {
		return nil, err
	}
	s, ok, present, _ := licensePlan(raw, f)
	splice := func(at, end int, text string) []byte {
		return []byte(string(raw[:at]) + text + string(raw[end:]))
	}
	if ok {
		return splice(s.start, s.end, plan), nil
	}
	if present {
		return addPlanKey(raw, f, plan)
	}
	switch f {
	case YAML:
		spans, _ := blockSpans(raw, f)
		at := 0
		for _, sp := range lineSpans(raw) {
			if licensePatterns.yamlHeader.MatchString(line(raw, sp)) || enginePatterns.yamlHeader.MatchString(line(raw, sp)) {
				at = lineEnd(raw, sp)
			}
		}
		if len(spans) > 0 {
			at = lineEnd(raw, spans[len(spans)-1])
		}
		text := fmt.Sprintf("license:\n  plan: %q\n", plan)
		if at > 0 && raw[at-1] != '\n' {
			text = "\n" + text
		}
		return splice(at, at, text), nil
	case TOML:
		text := fmt.Sprintf("\n[license]\nplan = %q\n", plan)
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			text = "\n" + text
		}
		return splice(len(raw), len(raw), text), nil
	case JSON:
		loc := enginePatterns.jsonBlock.FindIndex(raw)
		if loc == nil {
			return nil, errors.New("the \"engine\" object must hold only plain values")
		}
		return splice(loc[1], loc[1], fmt.Sprintf(",\n  \"license\": {\"plan\": %q}", plan)), nil
	}
	return nil, fmt.Errorf("unknown settings format %q", f)
}

// addPlanKey inserts plan into a license block that has no plan key.
func addPlanKey(raw []byte, f Format, plan string) ([]byte, error) {
	switch f {
	case YAML, TOML:
		header, text := licensePatterns.yamlHeader, fmt.Sprintf("  plan: %q\n", plan)
		if f == TOML {
			header, text = licensePatterns.tomlHead, fmt.Sprintf("plan = %q\n", plan)
		}
		for _, sp := range lineSpans(raw) {
			if header.MatchString(line(raw, sp)) {
				at := lineEnd(raw, sp)
				if at == len(raw) && (at == 0 || raw[at-1] != '\n') {
					text = "\n" + text
				}
				return []byte(string(raw[:at]) + text + string(raw[at:])), nil
			}
		}
	case JSON:
		loc := licensePatterns.jsonBlock.FindSubmatchIndex(raw)
		if loc != nil {
			text := fmt.Sprintf("\"plan\": %q", plan)
			if strings.TrimSpace(string(raw[loc[2]:loc[3]])) != "" {
				text += ", "
			}
			return []byte(string(raw[:loc[2]]) + text + string(raw[loc[2]:])), nil
		}
	}
	return nil, errors.New("the license block could not be edited")
}

// PlanOnlyChange refuses unless new is old with license.plan written to a
// different valid plan and nothing else changed.
func PlanOnlyChange(old, new []byte, f Format) error {
	ol, err := ReadLicense(old, f)
	if err != nil {
		return fmt.Errorf("the base settings: %w", err)
	}
	nl, err := ReadLicense(new, f)
	if err != nil {
		return err
	}
	if nl.Plan == "" || nl.Plan == ol.Plan {
		return errors.New("license.plan did not change")
	}
	moved, err := SetPlan(old, f, nl.Plan)
	if err != nil {
		return err
	}
	if string(moved) != string(new) {
		return errors.New("the settings change touches more than license.plan")
	}
	return nil
}
