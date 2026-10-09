// Package packindex reads a pack's index.json, as ClaudinitePacks'
// release workflow writes it to the vendored branch and R2, and picks the
// version a member should hold.
//
// The reader contract (ClaudinitePacks docs/release.md): unknown top-level
// and entry fields are ignored, so the writer may add one without an engine
// release, and only an unknown "v" is refused. The signature over the
// bytes is checked elsewhere (packaging/sign.VerifyPackIndex), before Decode.
package packindex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// Channels an entry may carry.
const (
	Stable = "stable"
	Canary = "canary"
)

// Entry is one published version of the pack.
type Entry struct {
	Version          string   `json:"version"`
	SHA256           string   `json:"sha256"`
	Size             int64    `json:"size"`
	MinEngineVersion string   `json:"minEngineVersion"`
	Requires         []string `json:"requires"`
	Channel          string   `json:"channel"`
	Revoked          bool     `json:"revoked"`
	PublishedAt      string   `json:"publishedAt"`
	SourceCommit     string   `json:"sourceCommit"`
}

// Index is a decoded index.json.
type Index struct {
	V        int
	Pack     string
	Serial   int64
	Versions []Entry
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Decode reads index.json, refusing a "v" other than 1, a serial that is
// not a positive integer, a missing pack, a channel outside canary and
// stable, and a version ComparePack cannot read.
func Decode(raw []byte) (Index, error) {
	var top struct {
		V        json.RawMessage `json:"v"`
		Pack     string          `json:"pack"`
		Serial   json.RawMessage `json:"serial"`
		Versions []Entry         `json:"versions"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&top); err != nil {
		return Index{}, fmt.Errorf("pack index: %w", err)
	}
	if string(top.V) != "1" {
		return Index{}, fmt.Errorf("pack index format v%s is not supported by this engine", orMissing(top.V))
	}
	serial, err := strconv.ParseInt(string(top.Serial), 10, 64)
	if err != nil || serial < 1 {
		return Index{}, fmt.Errorf("pack index serial %s is not a positive integer", orMissing(top.Serial))
	}
	if top.Pack == "" {
		return Index{}, errors.New("pack index names no pack")
	}
	for _, e := range top.Versions {
		if !version.ValidPack(e.Version) {
			return Index{}, fmt.Errorf("pack index %s: version %q is not dot-separated numbers", top.Pack, e.Version)
		}
		if e.Channel != Stable && e.Channel != Canary {
			return Index{}, fmt.Errorf("pack index %s %s: channel %q is neither canary nor stable", top.Pack, e.Version, e.Channel)
		}
		if !hex64.MatchString(e.SHA256) || e.Size < 0 {
			return Index{}, fmt.Errorf("pack index %s %s: sha256 and size do not describe an archive", top.Pack, e.Version)
		}
	}
	return Index{V: 1, Pack: top.Pack, Serial: serial, Versions: top.Versions}, nil
}

func orMissing(r json.RawMessage) string {
	if len(r) == 0 {
		return " (missing)"
	}
	return string(r)
}

// Entry returns the entry for v.
func (ix Index) Entry(v string) (Entry, bool) {
	for _, e := range ix.Versions {
		if e.Version == v {
			return e, true
		}
	}
	return Entry{}, false
}

// Want is what a member can take: its channel, its pinned engine, and the
// version it holds, empty for none.
type Want struct {
	Channel string
	Engine  string
	Held    string
}

// Skip is an entry passed over and why.
type Skip struct {
	Version, Reason string
}

// Skip reasons.
const (
	ReasonRevoked       = "revoked"
	ReasonCanary        = "canary"
	ReasonEngine        = "not for this engine"
	ReasonNotNewer      = "not newer"
	ReasonNoReplacement = "revoked, no replacement published"
)

// Choice is the entry to move to, nil for none, and the newest entry passed
// over, nil for none.
type Choice struct {
	Entry   *Entry
	Skipped *Skip
}

func newer(a, b string) bool {
	c, err := version.ComparePack(a, b)
	return err == nil && c > 0
}

// Select picks the newest entry that is not revoked, whose channel the
// member reads (stable; stable and canary on the canary channel), whose
// minEngineVersion the pinned engine meets, and that is newer than the
// held version. Skipped names the newest entry above the held version
// passed over for another reason, or the newest "not newer" one when
// nothing is due. A held version that is revoked with nothing due is reported as
// revoked with no replacement: a pack version is never rolled back.
func Select(ix Index, w Want) Choice {
	var c Choice
	var skip, notNewer *Skip
	for i := range ix.Versions {
		e := &ix.Versions[i]
		reason := ""
		switch {
		case e.Revoked:
			reason = ReasonRevoked
		case e.Channel == Canary && w.Channel != Canary:
			reason = ReasonCanary
		case !engineMeets(e.MinEngineVersion, w.Engine):
			reason = ReasonEngine
		case w.Held != "" && !newer(e.Version, w.Held):
			reason = ReasonNotNewer
		}
		switch {
		case reason == "":
			if c.Entry == nil || newer(e.Version, c.Entry.Version) {
				c.Entry = e
			}
		case reason == ReasonNotNewer:
			if notNewer == nil || newer(e.Version, notNewer.Version) {
				notNewer = &Skip{e.Version, reason}
			}
		case w.Held != "" && !newer(e.Version, w.Held):
			// Passed over below what the member already holds: nothing to say.
		default:
			if skip == nil || newer(e.Version, skip.Version) {
				skip = &Skip{e.Version, reason}
			}
		}
	}
	c.Skipped = skip
	if c.Entry == nil {
		if held, ok := ix.Entry(w.Held); ok && held.Revoked {
			c.Skipped = &Skip{held.Version, ReasonNoReplacement}
		} else if skip == nil {
			c.Skipped = notNewer
		}
	}
	return c
}

func engineMeets(minEngine, pin string) bool {
	m, err := version.ParseMinEngineVersion(minEngine)
	return err == nil && m.Satisfies(pin)
}
