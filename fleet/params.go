package fleet

import (
	"regexp"
	"strings"
)

// ContextEnv carries a work item's Context lines to its code-work.
const ContextEnv = "CLAUDINITE_CONTEXT"

var paramKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ParseParamBag reads the operator's parameters out of an item's Context:
// a token is a parameter only when it looks like one, a SHOUTING key with
// an optional value, so the prose an item is born with is never read as
// keys. Values stay strings (DRY_RUN=false is "false"); a bare key is
// "true". Tokens split on commas and newlines, so a list value is
// space-separated.
func ParseParamBag(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range regexp.MustCompile(`[,\n]`).Split(raw, -1) {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		key, value, hasValue := strings.Cut(token, "=")
		key = strings.TrimSpace(key)
		if !paramKey.MatchString(key) {
			continue
		}
		if !hasValue {
			out[key] = "true"
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}
