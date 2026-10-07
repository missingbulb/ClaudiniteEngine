// Package dashdesc reads a pack's dashboard descriptor the way the
// dashboard page's own reader does: the closed vocabulary of kinds and
// sources, a kind the reader predates kept as an unknown widget, the
// repo card's six-widget clip, the
// label and noun caps, and one named fault for a file the page cannot use.
// Parse is the page's parseDescriptor (packs/claudinite-dashboard/src/
// read/contributions.mjs at missingbulb/Claudinite@057841ac) verdict for
// verdict, and Problems is the descriptor-usable check's findings
// over the same file, so the page's copy, the shelf's test and the check
// are held to one reader through `cn dashboard descriptor`.
package dashdesc

import (
	"unicode/utf16"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/jsjson"
)

// The closed vocabulary. A kind outside Kinds renders as a widget saying
// the page predates its descriptor; a source outside Sources reads as
// generated.
var (
	Kinds   = []string{"stat", "event", "window", "list"}
	Sources = []string{"generated", "latest-release", "repo-stars"}
)

// The renderer-owned budgets.
const (
	MaxRepoWidgets = 6
	MaxListItems   = 5
	MaxLabel       = 34
	MaxText        = 46
	MaxNoun        = 16
)

// File is a pack's descriptor name.
const File = "dashboard.json"

// Widget is one normalised widget. Kind is nil when the descriptor's kind
// is not a string; Glyph is nil unless it is one code point.
type Widget struct {
	ID     string  `json:"id"`
	Kind   *string `json:"kind"`
	Known  bool    `json:"known"`
	Label  string  `json:"label"`
	Noun   string  `json:"noun"`
	Glyph  *string `json:"glyph"`
	Source string  `json:"source"`
}

// Descriptor is a parsed descriptor: Fault is the one reason the page
// cannot use it, and then nothing else is set.
type Descriptor struct {
	Pack    string
	Fault   string
	Widgets []Widget
	Repo    []string
}

// Widget is the descriptor's widget id, false when it declares none.
func (d Descriptor) Widget(id string) (Widget, bool) {
	for _, w := range d.Widgets {
		if w.ID == id {
			return w, true
		}
	}
	return Widget{}, false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// isJSWhitespace is a character String#trim removes.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func trim(s string) string {
	r := []rune(s)
	i, j := 0, len(r)
	for i < j && isJSWhitespace(r[i]) {
		i++
	}
	for j > i && isJSWhitespace(r[j-1]) {
		j--
	}
	return string(r[i:j])
}

// clip is the page's clip: trimmed, and past max UTF-16 units cut to
// max-1 with the elision shown.
func clip(s string, max int) string {
	u := utf16.Encode([]rune(trim(s)))
	if len(u) <= max {
		return string(utf16.Decode(u))
	}
	return string(utf16.Decode(u[:max-1])) + "…"
}

// text is `String(v ?? fallback)`: absent and null fall back.
func text(o jsjson.Value, key, fallback string) string {
	v, ok := o.Prop(key)
	if !ok || v.Kind == jsjson.Null {
		return fallback
	}
	return jsjson.StringOfValue(v)
}

func normalise(raw jsjson.Value) (Widget, bool) {
	if raw.Kind != jsjson.Object {
		return Widget{}, false
	}
	id, ok := raw.Prop("id")
	if !ok || id.Kind != jsjson.String || id.Str == "" {
		return Widget{}, false
	}
	w := Widget{ID: id.Str, Source: "generated"}
	if k, ok := raw.Prop("kind"); ok && k.Kind == jsjson.String {
		kind := k.Str
		w.Kind, w.Known = &kind, contains(Kinds, kind)
	}
	w.Label = clip(text(raw, "label", id.Str), MaxLabel)
	w.Noun = clip(text(raw, "noun", ""), MaxNoun)
	if g, ok := raw.Prop("glyph"); ok && g.Kind == jsjson.String {
		if r := []rune(trim(g.Str)); len(r) == 1 {
			glyph := string(r)
			w.Glyph = &glyph
		}
	}
	if s, ok := raw.Prop("source"); ok && s.Kind == jsjson.String && contains(Sources, s.Str) {
		w.Source = s.Str
	}
	return w, true
}

// list is a property when it is an array.
func list(o jsjson.Value, key string) []jsjson.Value {
	if v, ok := o.Prop(key); ok && v.Kind == jsjson.Array {
		return v.Arr
	}
	return nil
}

// Parse reads a descriptor's text for pack.
func Parse(text []byte, pack string) Descriptor {
	if msg := jsjson.SyntaxError(string(text)); msg != "" {
		return Descriptor{Pack: pack, Fault: "its dashboard.json is not valid JSON — " + msg}
	}
	doc, err := jsjson.Decode(text)
	if err != nil {
		return Descriptor{Pack: pack, Fault: "its dashboard.json is not valid JSON — " + err.Error()}
	}
	return FromDoc(doc, pack)
}

// FromDoc is Parse over an already parsed document.
func FromDoc(doc jsjson.Value, pack string) Descriptor {
	if doc.Kind != jsjson.Object {
		return Descriptor{Pack: pack, Fault: "its dashboard.json is not an object"}
	}
	if w, ok := doc.Prop("widgets"); !ok || w.Kind != jsjson.Array {
		return Descriptor{Pack: pack, Fault: "its dashboard.json declares no widgets array"}
	}
	d := Descriptor{Pack: pack, Widgets: []Widget{}}
	for _, raw := range list(doc, "widgets") {
		if w, ok := normalise(raw); ok {
			if _, seen := d.Widget(w.ID); !seen {
				d.Widgets = append(d.Widgets, w)
			}
		}
	}
	if len(d.Widgets) == 0 {
		return Descriptor{Pack: pack, Fault: "its dashboard.json declares no usable widget"}
	}
	pick := func(ids []jsjson.Value) []string {
		out := []string{}
		for _, id := range ids {
			if _, ok := d.Widget(id.Str); ok && id.Kind == jsjson.String {
				out = append(out, id.Str)
			}
		}
		return out
	}
	d.Repo = pick(list(doc, "repo"))
	if len(d.Repo) > MaxRepoWidgets {
		d.Repo = d.Repo[:MaxRepoWidgets]
	}
	return d
}
