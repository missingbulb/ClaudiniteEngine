package fleet

import (
	"regexp"
	"strings"
)

// TokenEnv is the one credential every sweep reads.
const TokenEnv = "FLEET_GITHUB_TOKEN"

// Sweeps, by the task ids that run them.
const (
	SweepRoster    = "fleet-roster"
	SweepAddPacks  = "fleet-add-missing-packs"
	SweepPackSeeds = "fleet-pack-seeds"
	SweepUpdate    = "fleet-update"
)

// Permission is one fine-grained PAT permission: what the grant must be
// (the widest any sweep needs), why, what each sweep alone uses, and the
// endpoints a 403 is attributed back to it by.
type Permission struct {
	Permission string            `json:"permission"`
	Access     string            `json:"access"`
	Why        string            `json:"why"`
	Sweeps     map[string]string `json:"sweeps"`
	endpoints  []*regexp.Regexp
}

// Permissions is the one statement of what FLEET_GITHUB_TOKEN must be
// granted; every message about the token and the handover step render
// from it. A person grants the token once, so a missing secret names the
// union, never one sweep's subset.
var Permissions = []Permission{
	{
		Permission: "Metadata", Access: "read",
		Why:       "every sweep enumerates the owner's repositories before it does anything else",
		Sweeps:    map[string]string{SweepRoster: "read", SweepAddPacks: "read", SweepPackSeeds: "read", SweepUpdate: "read"},
		endpoints: []*regexp.Regexp{regexp.MustCompile(`^/user/repos`), regexp.MustCompile(`^/repos/[^/]+/[^/]+(\?|$)`)},
	},
	{
		Permission: "Contents", Access: "read and write",
		Why:       "every sweep reads each member's declaration; the pack-seed sweep writes one back, so read alone is not enough",
		Sweeps:    map[string]string{SweepRoster: "read", SweepAddPacks: "read", SweepPackSeeds: "read and write", SweepUpdate: "read"},
		endpoints: []*regexp.Regexp{regexp.MustCompile(`/contents/`)},
	},
	{
		Permission: "Issues", Access: "read and write",
		Why:       "the roster and seed sweeps converge labelled issues in this repo, and the missing-packs sweep files a work-list issue in each member",
		Sweeps:    map[string]string{SweepRoster: "read and write", SweepAddPacks: "read and write", SweepPackSeeds: "read and write"},
		endpoints: []*regexp.Regexp{regexp.MustCompile(`/issues(/|\?|$)`), regexp.MustCompile(`/labels(/|\?|$)`)},
	},
	{
		Permission: "Actions", Access: "read and write",
		Why:       "the two fan-out sweeps dispatch another member's scheduler workflow, which is an Actions write",
		Sweeps:    map[string]string{SweepAddPacks: "read and write", SweepUpdate: "read and write"},
		endpoints: []*regexp.Regexp{regexp.MustCompile(`/actions/`), regexp.MustCompile(`/dispatches(\?|$)`)},
	},
}

// Grant is the whole grant in one line: "Metadata read, Contents read and
// write, …".
func Grant() string {
	parts := make([]string, 0, len(Permissions))
	for _, p := range Permissions {
		parts = append(parts, p.Permission+" "+p.Access)
	}
	return strings.Join(parts, ", ")
}

// GrantFor is what one sweep uses, for the explanatory half of a message;
// never what to grant.
func GrantFor(sweep string) string {
	var parts []string
	for _, p := range Permissions {
		if a, ok := p.Sweeps[sweep]; ok {
			parts = append(parts, p.Permission+" "+a)
		}
	}
	return strings.Join(parts, ", ")
}

// MissingTokenError is the sentence a sweep fails with when the secret is
// absent; detail is the sweep's own note on what it cannot do without it.
func MissingTokenError(sweep, detail string) string {
	uses := ""
	if u := GrantFor(sweep); u != "" {
		uses = " (this one, " + sweep + ", uses " + u + ")"
	}
	return TokenEnv + " is not set. Add a repo secret holding a fine-grained PAT on this account, " +
		"ALL repositories, granted: " + Grant() + ". " +
		"Grant the whole set — it is one token for the whole pack, and a subset breaks a different " +
		"sweep later" + uses + ". " + detail
}

// PermissionFor is the permission a 403 on path most likely lacks, nil
// when no row claims the path.
func PermissionFor(path string) *Permission {
	for i := range Permissions {
		for _, re := range Permissions[i].endpoints {
			if re.MatchString(path) {
				return &Permissions[i]
			}
		}
	}
	return nil
}

// ForbiddenHint is the tail a 403 on path carries; empty for a path no
// row claims, since a wrong guess is worse than none.
func ForbiddenHint(path string) string {
	p := PermissionFor(path)
	if p == nil {
		return ""
	}
	return " — the " + TokenEnv + " PAT is most likely missing " + p.Permission + ": " + p.Access + " (" + p.Why + ")"
}

// Step is the handover checkbox the adopting session files.
type Step struct {
	Step   string `json:"step"`
	Breaks string `json:"breaks"`
	Done   string `json:"done"`
}

// HandoverStep is the complete grant as the one human step, which the
// pack's adoptionHandover is held to.
func HandoverStep() Step {
	return Step{
		Step: "Create a fine-grained PAT on this account covering ALL repositories, granted " + Grant() + ", " +
			"and add it to this repo as the Actions secret " + TokenEnv + ". Grant every permission listed, not the " +
			"subset the first sweep you run needs.",
		Breaks: "every claudinite-fleet-sheepdog sweep fails — and a token short one permission fails only on the sweep that needs it, " +
			"which can be a week later, on the one sweep that writes or dispatches",
		Done: "the secret exists and each claudinite-fleet-sheepdog task's next run is green",
	}
}
