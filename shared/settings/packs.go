package settings

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The packs block sits beside engine and declares which packs the repo
// runs and from which channel:
//
//	packs:                       [packs]                        "packs": {
//	  channel: "canary"          channel = "canary"               "channel": "canary",
//	  declared:                  declared = ["hello"]             "declared": ["hello"]
//	    - hello                                                 }
//
// ReadPacks reads it through the descriptor parsers (see parsed.go); the
// strict patterns below only locate where AddDeclared inserts an id, so the
// edit changes no other byte. An absent block declares nothing on the
// stable channel.

// Channels a member may read pack versions from.
const (
	ChannelStable = "stable"
	ChannelCanary = "canary"
)

// PackIDPattern is a pack id.
var PackIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Packs is the packs block.
type Packs struct {
	Channel string
	// Declared is the canon ids in declared order; a local pack is never
	// fetched, so it is kept apart in Local.
	Declared []string
	// Local is the names declared as local/<name>, in declared order.
	Local []string
	// Entries is every declared entry in order, canon and local.
	Entries []PackEntry
	// Present is false when the file holds no packs block.
	Present bool
}

// packsLayout is the block as read, with the offsets an edit needs.
type packsLayout struct {
	Packs
	// hasDeclared is true when the declared key is present.
	hasDeclared bool
	// insert is the offset where AddDeclared inserts, and prefix/suffix
	// wrap the new id there; see each format's reader.
	insert         int
	prefix, suffix string
}

var (
	yamlPacksHeader = regexp.MustCompile(`^packs:[ \t]*$`)
	yamlChannel     = regexp.MustCompile(`^([ \t]+)channel:[ \t]*"([^"]*)"[ \t]*$`)
	yamlDeclared    = regexp.MustCompile(`^([ \t]+)declared:[ \t]*$`)
	yamlItem        = regexp.MustCompile(`^([ \t]+)-[ \t]+("?)([^" \t]+)("?)[ \t]*$`)
	yamlComment     = regexp.MustCompile(`^[ \t]*(#.*)?$`)
	tomlPacksHeader = regexp.MustCompile(`^\[packs\][ \t]*$`)
	tomlChannel     = regexp.MustCompile(`^[ \t]*channel[ \t]*=[ \t]*"([^"]*)"[ \t]*$`)
	tomlDeclared    = regexp.MustCompile(`declared[ \t]*=[ \t]*\[([^\]]*)\]`)
	jsonPacksKey    = regexp.MustCompile(`"packs"[ \t]*:`)
	jsonPacksBlock  = regexp.MustCompile(`"packs"[ \t]*:[ \t\r\n]*\{([^{}]*)\}`)
	jsonChannel     = regexp.MustCompile(`"channel"[ \t\r\n]*:[ \t\r\n]*"([^"]*)"`)
	jsonDeclared    = regexp.MustCompile(`"declared"[ \t\r\n]*:[ \t\r\n]*\[([^\]]*)\]`)
	listItem        = regexp.MustCompile(`^[ \t\r\n]*"([^"]*)"[ \t\r\n]*$`)
)

func (l *packsLayout) setChannel(v string) error {
	if l.Channel != "" {
		return errors.New("packs.channel appears twice")
	}
	if v != ChannelStable && v != ChannelCanary {
		return fmt.Errorf("packs.channel must be \"stable\" or \"canary\", not %q", v)
	}
	l.Channel = v
	return nil
}

func (l *packsLayout) addID(id string) error {
	if !PackIDPattern.MatchString(id) && !localIDPattern.MatchString(id) {
		return fmt.Errorf("packs.declared: %q is not a pack id (lowercase letters, digits and dashes) or local/<name>", id)
	}
	for _, d := range l.Declared {
		if d == id {
			return fmt.Errorf("packs.declared names %s twice", id)
		}
	}
	l.Declared = append(l.Declared, id)
	return nil
}

// listItems reads the inside of a [ ... ] list of quoted ids, returning
// the offset just after the last item, or -1 when the list is empty.
func (l *packsLayout) listItems(inner string, base int) (int, error) {
	if strings.TrimSpace(inner) == "" {
		return -1, nil
	}
	end, at := -1, 0
	for _, part := range strings.Split(inner, ",") {
		m := listItem.FindStringSubmatchIndex(part)
		if m == nil {
			return 0, fmt.Errorf("packs.declared must be a list of quoted pack ids, not %q", strings.TrimSpace(part))
		}
		if err := l.addID(part[m[2]:m[3]]); err != nil {
			return 0, err
		}
		end = base + at + m[3] + 1
		at += len(part) + 1
	}
	return end, nil
}

func readPacksLayout(raw []byte, f Format) (packsLayout, error) {
	var l packsLayout
	var err error
	switch f {
	case YAML:
		err = l.readYAML(raw)
	case TOML:
		err = l.readTOML(raw)
	case JSON:
		err = l.readJSON(raw)
	default:
		err = fmt.Errorf("unknown settings format %q", f)
	}
	if err != nil {
		return packsLayout{}, err
	}
	if l.Channel == "" {
		l.Channel = ChannelStable
	}
	return l, nil
}

func line(raw []byte, s span) string { return strings.TrimSuffix(string(raw[s.start:s.end]), "\r") }

// lineEnd is the offset after s's newline, or the file's end.
func lineEnd(raw []byte, s span) int {
	if s.end < len(raw) {
		return s.end + 1
	}
	return s.end
}

func (l *packsLayout) readYAML(raw []byte) error {
	var block []span
	headers, in := 0, false
	var headerSpan span
	for _, s := range lineSpans(raw) {
		text := line(raw, s)
		switch {
		case yamlPacksHeader.MatchString(text):
			headers++
			in = true
			headerSpan = s
		case in && yamlEnd.MatchString(text):
			in = false
		case in:
			block = append(block, s)
		}
	}
	if headers > 1 {
		return errors.New("the settings hold more than one packs block")
	}
	if headers == 0 {
		l.insert, l.prefix, l.suffix = len(raw), "packs:\n  declared:\n    - ", "\n"
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			l.prefix = "\n" + l.prefix
		}
		return nil
	}
	l.Present = true
	last := headerSpan
	keyIndent := "  "
	inList := false
	for _, s := range block {
		text := line(raw, s)
		if m := yamlItem.FindStringSubmatch(text); m != nil && inList {
			if m[2] != m[4] {
				return fmt.Errorf("packs.declared: unbalanced quotes in %q", strings.TrimSpace(text))
			}
			if err := l.addID(m[3]); err != nil {
				return err
			}
			l.insert, l.prefix, l.suffix = lineEnd(raw, s), m[1]+"- ", "\n"
			last = s
			continue
		}
		inList = false
		switch m1, m2 := yamlChannel.FindStringSubmatch(text), yamlDeclared.FindStringSubmatch(text); {
		case m1 != nil:
			if err := l.setChannel(m1[2]); err != nil {
				return err
			}
			keyIndent = m1[1]
			last = s
		case m2 != nil:
			if l.hasDeclared {
				return errors.New("packs.declared appears twice")
			}
			l.hasDeclared, inList, keyIndent = true, true, m2[1]
			l.insert, l.prefix, l.suffix = lineEnd(raw, s), m2[1]+"  - ", "\n"
			last = s
		case yamlComment.MatchString(text):
		default:
			return fmt.Errorf("the packs block holds %q; it takes only channel: \"...\" and declared: with one \"- <id>\" line per pack", strings.TrimSpace(text))
		}
	}
	if !l.hasDeclared {
		l.insert, l.prefix, l.suffix = lineEnd(raw, last), keyIndent+"declared:\n"+keyIndent+"  - ", "\n"
		if l.insert == len(raw) && (len(raw) == 0 || raw[len(raw)-1] != '\n') {
			l.prefix = "\n" + l.prefix
		}
	}
	return nil
}

func (l *packsLayout) readTOML(raw []byte) error {
	var block []span
	headers, in := 0, false
	var headerSpan span
	for _, s := range lineSpans(raw) {
		text := line(raw, s)
		switch {
		case tomlPacksHeader.MatchString(text):
			headers++
			in = true
			headerSpan = s
		case in && tomlEnd.MatchString(text):
			in = false
		case in:
			block = append(block, s)
		}
	}
	if headers > 1 {
		return errors.New("the settings hold more than one [packs] table")
	}
	if headers == 0 {
		l.insert, l.prefix, l.suffix = len(raw), "\n[packs]\ndeclared = [\"", "\"]\n"
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			l.prefix = "\n" + l.prefix
		}
		return nil
	}
	l.Present = true
	l.insert, l.prefix, l.suffix = lineEnd(raw, headerSpan), "declared = [\"", "\"]\n"
	if len(block) == 0 {
		return nil
	}
	start, end := block[0].start, block[len(block)-1].end
	body := string(raw[start:end])
	var rest strings.Builder
	at := 0
	for _, m := range tomlDeclared.FindAllStringSubmatchIndex(body, -1) {
		if l.hasDeclared {
			return errors.New("packs.declared appears twice")
		}
		l.hasDeclared = true
		last, err := l.listItems(body[m[2]:m[3]], start+m[2])
		if err != nil {
			return err
		}
		if last < 0 {
			l.insert, l.prefix, l.suffix = start+m[2], "\"", "\""
		} else {
			l.insert, l.prefix, l.suffix = last, ", \"", "\""
		}
		rest.WriteString(body[at:m[0]])
		at = m[1]
	}
	rest.WriteString(body[at:])
	for _, text := range strings.Split(rest.String(), "\n") {
		text = strings.TrimSuffix(text, "\r")
		if m := tomlChannel.FindStringSubmatch(text); m != nil {
			if err := l.setChannel(m[1]); err != nil {
				return err
			}
			continue
		}
		if !yamlComment.MatchString(text) {
			return fmt.Errorf("the [packs] table holds %q; it takes only channel = \"...\" and declared = [\"<id>\", ...]", strings.TrimSpace(text))
		}
	}
	return nil
}

func (l *packsLayout) readJSON(raw []byte) error {
	flat := strings.NewReplacer("\r", "", "\n", "").Replace(string(raw))
	switch n := len(jsonPacksKey.FindAllString(flat, -1)); n {
	case 0:
		loc := enginePatterns.jsonBlock.FindIndex(raw)
		if loc == nil {
			l.insert = -1
			return nil
		}
		l.insert, l.prefix, l.suffix = loc[1], ",\n  \"packs\": {\"declared\": [\"", "\"]}"
		return nil
	case 1:
	default:
		return fmt.Errorf("the settings must hold at most one \"packs\" object, found %d", n)
	}
	loc := jsonPacksBlock.FindSubmatchIndex(raw)
	if loc == nil {
		return errors.New("the \"packs\" object must hold only \"channel\" and a \"declared\" list")
	}
	l.Present = true
	start := loc[2]
	body := string(raw[loc[2]:loc[3]])
	l.insert, l.prefix, l.suffix = start, "\"declared\": [\"", "\"], "
	if strings.TrimSpace(body) == "" {
		l.suffix = "\"]"
	}
	var rest strings.Builder
	at := 0
	for _, m := range jsonDeclared.FindAllStringSubmatchIndex(body, -1) {
		if l.hasDeclared {
			return errors.New("packs.declared appears twice")
		}
		l.hasDeclared = true
		last, err := l.listItems(body[m[2]:m[3]], start+m[2])
		if err != nil {
			return err
		}
		if last < 0 {
			l.insert, l.prefix, l.suffix = start+m[2], "\"", "\""
		} else {
			l.insert, l.prefix, l.suffix = last, ", \"", "\""
		}
		rest.WriteString(body[at:m[0]])
		at = m[1]
	}
	rest.WriteString(body[at:])
	left := rest.String()
	for _, m := range jsonChannel.FindAllStringSubmatch(left, -1) {
		if err := l.setChannel(m[1]); err != nil {
			return err
		}
	}
	left = jsonChannel.ReplaceAllString(left, "")
	if strings.Trim(left, " \t\r\n,") != "" || strings.Contains(left, ",,") {
		return fmt.Errorf("the \"packs\" object holds %q; it takes only \"channel\" and \"declared\"", strings.TrimSpace(left))
	}
	return nil
}

// ReadPacks reads the packs block: the channel (stable when absent) and the
// declared entries in order.
func ReadPacks(raw []byte, f Format) (Packs, error) {
	p, err := ParseFile(raw, f)
	if err != nil {
		return Packs{}, err
	}
	return p.Packs, nil
}

// AddDeclared appends id to the declared packs by inserting it, creating
// the block or the list when absent, and changes no other byte.
func AddDeclared(raw []byte, f Format, id string) ([]byte, error) {
	if !PackIDPattern.MatchString(id) {
		return nil, fmt.Errorf("%q is not a pack id (lowercase letters, digits and dashes)", id)
	}
	p, err := ReadPacks(raw, f)
	if err != nil {
		return nil, err
	}
	for _, e := range p.Entries {
		if e.Object {
			return nil, fmt.Errorf("packs.declared holds an entry object (%s); add %s to the list by hand, since a line edit cannot keep an object entry intact", e.Token(), id)
		}
		if e.Token() == id {
			return nil, fmt.Errorf("%s is already declared", id)
		}
	}
	l, err := readPacksLayout(raw, f)
	if err != nil {
		return nil, fmt.Errorf("packs.declared parses but is not in the one-id-per-entry layout a line edit can extend (%v); add %s by hand", err, id)
	}
	if l.insert < 0 {
		return nil, errors.New("the settings hold no \"engine\" object to add a packs block beside")
	}
	out := make([]byte, 0, len(raw)+len(id)+64)
	out = append(out, raw[:l.insert]...)
	out = append(out, l.prefix+id+l.suffix...)
	return append(out, raw[l.insert:]...), nil
}
