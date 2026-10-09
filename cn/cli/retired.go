package main

import (
	"fmt"
	"io"
	"strings"
)

// retired maps a spelling the trimmed command surface dropped to today's,
// and the notice that names the replacement. Member workflows and vendored
// pack skills still carry the old spellings until their own updates move
// them, so each keeps running for one convergence window.
//
// @legacy-tolerance advisory:none retire:#167
func retired(args []string) ([]string, string) {
	if len(args) == 0 {
		return args, ""
	}
	sub, rest := "", []string(nil)
	if len(args) > 1 {
		sub, rest = args[1], args[2:]
	}
	var to []string
	switch {
	case args[0] == "init":
		to = initAsAdopt(args[1:])
	case args[0] == "settings" && sub == "answer":
		to = answerAsAdopt(rest)
	case args[0] == "pack" && sub == "new":
		to = append([]string{"adopt"}, rest...)
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			to = append([]string{"adopt", "local/" + rest[0]}, rest[1:]...)
		}
	case args[0] == "rules-index" && !contains(args[1:], "--check"):
		to = append([]string{"adopt"}, args[1:]...)
	case args[0] == "tasks" && sub == "list":
		to = append([]string{"check", "list", "--tasks"}, rest...)
	case args[0] == "tasks" && sub == "flat" && contains(rest, "--write"):
		to = append([]string{"adopt"}, without(rest, "--write")...)
	case args[0] == "schedule" && sub == "drain":
		to = append([]string{"execute", "dispatch"}, rest...)
	case args[0] == "execute" && sub == "continue":
		to = append([]string{"execute", "dispatch", "--continue"}, rest...)
	case args[0] == "provenance" && sub == "brief":
		to = append([]string{"provenance", "backfill"}, rest...)
	case args[0] == "provenance" && sub == "apply" && len(rest) >= 2:
		to = append([]string{"provenance", "backfill", rest[0], "--apply", rest[1]}, rest[2:]...)
	default:
		return args, ""
	}
	name := args[0]
	if sub != "" && !strings.HasPrefix(sub, "-") && args[0] != "init" && args[0] != "rules-index" {
		name += " " + sub
	}
	return to, fmt.Sprintf("cn: `%s` is retired; ran `cn %s` instead", name, strings.Join(to, " "))
}

// initAsAdopt is `init --packs IDS …` as `adopt IDS …`.
func initAsAdopt(args []string) []string {
	out := []string{"adopt"}
	var ids string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "--packs" || a == "-packs") && i+1 < len(args):
			ids = args[i+1]
			i++
		case strings.HasPrefix(a, "--packs=") || strings.HasPrefix(a, "-packs="):
			ids = a[strings.Index(a, "=")+1:]
		default:
			out = append(out, a)
		}
	}
	if ids == "" {
		return out
	}
	return append([]string{"adopt", ids}, out[1:]...)
}

// answerAsAdopt is `settings answer ADDR [--] TEXT …` as
// `adopt --answer ADDR=TEXT …`.
func answerAsAdopt(args []string) []string {
	var pos, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--" && len(pos) < 2:
			j := i + 1
			for ; j < len(args) && len(pos) < 2; j++ {
				pos = append(pos, args[j])
			}
			rest = append(rest, args[j:]...)
			i = len(args)
		case (a == "--repo" || a == "-repo") && i+1 < len(args):
			rest = append(rest, a, args[i+1])
			i++
		case strings.HasPrefix(a, "-"):
			rest = append(rest, a)
		case len(pos) < 2:
			pos = append(pos, a)
		default:
			rest = append(rest, a)
		}
	}
	if len(pos) != 2 {
		return append([]string{"adopt", "--answer", strings.Join(pos, "=")}, rest...)
	}
	return append([]string{"adopt", "--answer", pos[0] + "=" + pos[1]}, rest...)
}

func contains(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || a == "-"+strings.TrimPrefix(flag, "--") {
			return true
		}
	}
	return false
}

func without(args []string, flag string) []string {
	var out []string
	for _, a := range args {
		if a != flag && a != "-"+strings.TrimPrefix(flag, "--") {
			out = append(out, a)
		}
	}
	return out
}

// noteRetired rewrites a retired spelling and says so on stderr.
func noteRetired(args []string, stderr io.Writer) []string {
	to, note := retired(args)
	if note != "" {
		fmt.Fprintln(stderr, note)
	}
	return to
}
