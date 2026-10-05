package settings

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The license block sat beside engine and named the repo's plan:
//
//	license:                     [license]                      "license": {"plan": "public"}
//	  plan: "public"             plan = "public"
//
// A repo needs no license and nothing reads the plan, so the block is a
// retired shape: a file that still carries it parses, verify names it,
// and the nightly engine update's pull request drops it.

// HasRetiredLicense reports whether the file still carries the license
// block, whatever it holds.
//
// @legacy-tolerance advisory:license-plan retire:#TBD
func HasRetiredLicense(raw []byte, f Format) bool {
	switch f {
	case YAML, TOML:
		header := licensePatterns.yamlHeader
		if f == TOML {
			header = licensePatterns.tomlHead
		}
		for _, s := range lineSpans(raw) {
			if header.MatchString(strings.TrimSuffix(line(raw, s), "\r")) {
				return true
			}
		}
		return false
	case JSON:
		return licensePatterns.jsonKey.Match(raw)
	}
	return false
}

var (
	jsonLeadingComma  = regexp.MustCompile(`,[ \t\r\n]*$`)
	jsonTrailingComma = regexp.MustCompile(`^[ \t\r\n]*,[ \t\r\n]*`)
)

// DropLicense removes the license block and changes no other byte; a file
// without one comes back as it was.
//
// @legacy-tolerance advisory:license-plan retire:#TBD
func DropLicense(raw []byte, f Format) ([]byte, error) {
	if !HasRetiredLicense(raw, f) {
		return raw, nil
	}
	var out []byte
	switch f {
	case YAML, TOML:
		header, end := licensePatterns.yamlHeader, yamlEnd
		if f == TOML {
			header, end = licensePatterns.tomlHead, tomlEnd
		}
		spans := lineSpans(raw)
		from, to := -1, len(raw)
		for _, s := range spans {
			l := strings.TrimSuffix(line(raw, s), "\r")
			if from < 0 {
				if header.MatchString(l) {
					from = s.start
				}
				continue
			}
			if end.MatchString(l) {
				to = s.start
				break
			}
		}
		out = append(append([]byte{}, raw[:from]...), raw[to:]...)
		if to == len(raw) {
			// The block was last: the blank line that set it apart goes too.
			trimmed := strings.TrimRight(string(out), "\r\n")
			if trimmed != "" {
				out = []byte(trimmed + "\n")
			}
		}
	case JSON:
		loc := licensePatterns.jsonBlock.FindIndex(raw)
		if loc == nil {
			return nil, errors.New("the \"license\" object must hold only plain values to be dropped")
		}
		start, stop := loc[0], loc[1]
		if c := jsonLeadingComma.FindIndex(raw[:start]); c != nil {
			start = c[0]
		} else if c := jsonTrailingComma.FindIndex(raw[stop:]); c != nil {
			stop += c[1]
		}
		out = append(append([]byte{}, raw[:start]...), raw[stop:]...)
	default:
		return nil, fmt.Errorf("unknown settings format %q", f)
	}
	if HasRetiredLicense(out, f) {
		return nil, errors.New("the settings hold the license block more than once")
	}
	if _, err := ReadEngine(out, f); err != nil {
		return nil, fmt.Errorf("dropping the license block: %w", err)
	}
	return out, nil
}
