package provenance

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/skillfm"
)

// IO reads a tree over repo-relative, slash-separated paths. ListDir
// names the entries one segment below a directory, false where nothing
// is under it.
type IO interface {
	Exists(p string) bool
	Read(p string) (string, bool)
	ListDir(p string) ([]string, bool)
}

// ListIO is an IO whose directories are read off a file list, so an audit
// walks exactly the files a run scans, and whose files are read and
// probed by the two functions.
type ListIO struct {
	Files    []string
	ReadFile func(p string) (string, bool)
	Has      func(p string) bool
}

// Exists probes p.
func (l ListIO) Exists(p string) bool { return l.Has(p) }

// Read reads p.
func (l ListIO) Read(p string) (string, bool) { return l.ReadFile(p) }

// ListDir names what the file list holds one segment below p.
func (l ListIO) ListDir(p string) ([]string, bool) { return ListFrom(l.Files, p) }

// ListFrom names the entries one segment below p in files, false when
// nothing is under it.
func ListFrom(files []string, p string) ([]string, bool) {
	seen := map[string]bool{}
	var out []string
	prefix := p + "/"
	for _, f := range files {
		f = strings.ReplaceAll(f, "\\", "/")
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		name := strings.SplitN(f[len(prefix):], "/", 2)[0]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, len(out) > 0
}

func read(io IO, p string) string {
	s, _ := io.Read(p)
	return s
}

func isDir(io IO, p string) bool {
	_, ok := io.ListDir(p)
	return ok
}

func listDirs(io IO, p string) []string {
	names, _ := io.ListDir(p)
	var out []string
	for _, n := range names {
		if !strings.HasPrefix(n, ".") && isDir(io, p+"/"+n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func listFiles(io IO, p string) []string {
	names, _ := io.ListDir(p)
	var out []string
	for _, n := range names {
		if !isDir(io, p+"/"+n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Shape is what a SKILL.md says of itself: its declared Body ("" for
// none), the body Proposed from its shape, its bullets, and BodyOffset,
// how many lines the frontmatter takes.
type Shape struct {
	Body, Proposed string
	Bullets        []Block
	BodyOffset     int
}

// SkillShape reads a SKILL.md: bold-trigger bullets and no numbered steps
// propose guidelines, anything else a workflow.
func SkillShape(src string) Shape {
	bodyStart := 0
	if strings.HasPrefix(src, "---") {
		if end := strings.Index(src[3:], "\n---"); end >= 0 {
			end += 3
			if nl := strings.Index(src[end+1:], "\n"); nl >= 0 {
				bodyStart = end + 1 + nl + 1
			}
		}
	}
	bodyText := src[bodyStart:]
	bullets := RuleBlocks(bodyText, true)
	bodyLines := strings.Split(bodyText, "\n")
	steps := 0
	for _, l := range bodyLines {
		if numberedStep.MatchString(l) {
			steps++
		}
	}
	proposed := "workflow"
	if steps == 0 {
		for _, b := range bullets {
			if ruleBullet.MatchString(bodyLines[b.Start]) {
				proposed = "guidelines"
				break
			}
		}
	}
	return Shape{Body: skillfm.Read(src).Body, Proposed: proposed, Bullets: bullets, BodyOffset: strings.Count(src[:bodyStart], "\n")}
}

// Rule is one prose carrier: a RULES.md rule, or a skill's bullet (Skill
// set), 1-based lines.
type Rule struct {
	File, Skill             string
	Line, LastLine, EndLine int
	Trigger, Slug, Numeric  string
	Text                    string
}

// Skill is one skill directory of a pack.
type Skill struct {
	Name, File, Body, Proposed string
	Bullets                    []Rule
	Present                    bool
}

// Carrier is an element named by id: a check, a task or a declared rule,
// with the file that carries it.
type Carrier struct {
	ID, File string
	// Dir is a task's folder.
	Dir string
}

// Carriers are every carrier of one pack.
type Carriers struct {
	Rules, Guidelines    []Rule
	Skills               []Skill
	Checks, Tasks, Decls []Carrier
	// ManifestFile is the manifest's name in the pack, "" for none.
	ManifestFile string
}

// ManifestFiles are a manifest's spellings, the preferred first: the
// three this engine reads, then the Node engine's module.
var ManifestFiles = []string{"pack.json", "pack.yaml", "pack.toml", "pack.mjs"}

// ProseFile is a pack's prose; SkillsDir its skills; RuleDirs the Node
// engine's coded-check folders; GoChecksDir this engine's.
const (
	ProseFile   = "RULES.md"
	SkillsDir   = "skills"
	GoChecksDir = "checks"
)

// RuleDirs are the Node engine's coded-check folders.
var RuleDirs = []string{"worldRules", "workRules"}

var (
	mjsID      = regexp.MustCompile(`\bid\s*:\s*['"]([^'"]+)['"]`)
	mjsConstID = regexp.MustCompile(`\bconst\s+id\s*=\s*['"]([^'"]+)['"]`)
	mjsShort   = regexp.MustCompile(`[{,]\s*id\s*[,}]`)
	mjsFrom    = regexp.MustCompile(`\bfrom\s*['"](\.{1,2}/[^'"]+\.mjs)['"]`)
	goID       = regexp.MustCompile(`\bID:\s*"([^"]+)"`)
)

// checkIDsIn are the ids a Node rule module declares: each id: literal,
// and a const id an object takes by shorthand.
func checkIDsIn(src string) []string {
	var out []string
	for _, m := range mjsID.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	if c := mjsConstID.FindStringSubmatch(src); c != nil && mjsShort.MatchString(src) && !has(out, c[1]) {
		out = append(out, c[1])
	}
	return out
}

// checksOfModule are the checks a Node module carries, following an
// aggregator's relative imports inside the pack: the file declaring an
// id carries it.
func checksOfModule(io IO, file, within string, seen map[string]bool) []Carrier {
	if seen[file] {
		return nil
	}
	seen[file] = true
	src := read(io, file)
	var own []Carrier
	for _, id := range checkIDsIn(src) {
		own = append(own, Carrier{ID: id, File: file})
	}
	if len(own) > 0 {
		return own
	}
	dir := path.Dir(file)
	var out []Carrier
	ids := map[string]bool{}
	for _, m := range mjsFrom.FindAllStringSubmatch(src, -1) {
		resolved := path.Clean(dir + "/" + m[1])
		if within != "" && !strings.HasPrefix(resolved, within+"/") {
			continue
		}
		if !io.Exists(resolved) {
			continue
		}
		for _, c := range checksOfModule(io, resolved, within, seen) {
			if !ids[c.ID] {
				ids[c.ID] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// declaredIDs are the ids of a declared-checks file's entries; nil when
// it is absent or does not parse.
func declaredIDs(io IO, p string) []string {
	text, ok := io.Read(p)
	if !ok {
		return nil
	}
	var v any
	if strings.HasSuffix(p, ".json") {
		if json.Unmarshal([]byte(text), &v) != nil {
			return nil
		}
	} else {
		d, err := descriptor.ParseDocument([]byte(text), descriptor.FormatOf(p))
		if err != nil {
			return nil
		}
		v = d
	}
	list, _ := v.([]any)
	var out []string
	for _, d := range list {
		if m, ok := d.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

var declaredFiles = []string{"declared-checks.json", "declared-checks.yaml", "declared-checks.yml", "declared-checks.toml"}

// PackCarriers enumerates the pack at packDir: its RULES.md rules, its
// skills and a guidelines skill's bullets, its checks (Node rule modules,
// Go checks and declared checks, by id), its tasks, the rules a
// *-rules.json declares as data, and its manifest.
func PackCarriers(packDir string, io IO) Carriers {
	var c Carriers
	prose := packDir + "/" + ProseFile
	if io.Exists(prose) {
		for _, b := range RuleBlocks(read(io, prose), false) {
			c.Rules = append(c.Rules, Rule{File: prose, Line: b.Start + 1, LastLine: b.LastLine + 1, EndLine: b.End + 1, Trigger: b.Trigger, Slug: b.Slug, Numeric: b.Numeric, Text: b.Text})
		}
	}
	for _, name := range listDirs(io, packDir+"/"+SkillsDir) {
		dir := packDir + "/" + SkillsDir + "/" + name
		file := dir + "/SKILL.md"
		s := Skill{Name: name, File: file, Proposed: "workflow", Present: io.Exists(file)}
		if s.Present {
			sh := SkillShape(read(io, file))
			s.Body, s.Proposed = sh.Body, sh.Proposed
			for _, b := range sh.Bullets {
				s.Bullets = append(s.Bullets, Rule{File: file, Skill: name, Line: sh.BodyOffset + b.Start + 1, LastLine: sh.BodyOffset + b.LastLine + 1, EndLine: sh.BodyOffset + b.End + 1, Trigger: b.Trigger, Slug: b.Slug, Numeric: b.Numeric, Text: b.Text})
			}
		}
		c.Skills = append(c.Skills, s)
		if s.Body == "guidelines" {
			c.Guidelines = append(c.Guidelines, s.Bullets...)
		}
		if io.Exists(dir + "/checks.mjs") {
			c.Checks = append(c.Checks, checksOfModule(io, dir+"/checks.mjs", packDir, map[string]bool{})...)
		}
		for _, f := range declaredFiles {
			for _, id := range declaredIDs(io, dir+"/"+f) {
				c.Checks = append(c.Checks, Carrier{ID: id, File: dir + "/" + f})
			}
		}
	}
	for _, scope := range RuleDirs {
		for _, f := range listFiles(io, packDir+"/"+scope) {
			if !strings.HasSuffix(f, ".mjs") || strings.HasSuffix(f, ".test.mjs") {
				continue
			}
			p := packDir + "/" + scope + "/" + f
			for _, id := range checkIDsIn(read(io, p)) {
				c.Checks = append(c.Checks, Carrier{ID: id, File: p})
			}
		}
	}
	for _, f := range listFiles(io, packDir+"/"+GoChecksDir) {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		p := packDir + "/" + GoChecksDir + "/" + f
		for _, m := range goID.FindAllStringSubmatch(read(io, p), -1) {
			c.Checks = append(c.Checks, Carrier{ID: m[1], File: p})
		}
	}
	for _, f := range declaredFiles {
		for _, id := range declaredIDs(io, packDir+"/"+f) {
			c.Checks = append(c.Checks, Carrier{ID: id, File: packDir + "/" + f})
		}
	}
	for _, t := range listDirs(io, packDir+"/tasks") {
		dir := packDir + "/tasks/" + t
		jsonFile, mdFile := io.Exists(dir+"/task.json"), io.Exists(dir+"/task.md")
		if !jsonFile && !mdFile {
			continue
		}
		file := dir + "/task.md"
		if jsonFile {
			file = dir + "/task.json"
		}
		c.Tasks = append(c.Tasks, Carrier{ID: t, Dir: dir, File: file})
	}
	for _, f := range listFiles(io, packDir) {
		if !strings.HasSuffix(f, "-rules.json") {
			continue
		}
		var doc struct {
			Rules []map[string]any `json:"rules"`
		}
		if json.Unmarshal([]byte(read(io, packDir+"/"+f)), &doc) != nil {
			continue
		}
		for _, d := range doc.Rules {
			if id, ok := d["id"].(string); ok {
				c.Decls = append(c.Decls, Carrier{ID: id, File: packDir + "/" + f})
			}
		}
	}
	for _, m := range ManifestFiles {
		if io.Exists(packDir + "/" + m) {
			c.ManifestFile = m
			break
		}
	}
	return c
}

// File is one element's provenance file.
type File struct {
	ID, Path, Text string
	Entries        []Entry
	Problems       []Problem
	Status         string
	// Empty is pending history; ConvertedOnly a file the references
	// conversion filled and nothing has since, which owes a backfill too.
	Empty, ConvertedOnly bool
}

// Files are a pack's element files, by id in name order: a record kept
// beside them under another name (a version log) and the declined log
// are not elements.
func Files(packDir string, io IO) []File {
	dir := packDir + "/" + Dir
	var out []File
	for _, name := range listFiles(io, dir) {
		if !elementFile.MatchString(name) || name == DeclinedFile {
			continue
		}
		p := dir + "/" + name
		text := read(io, p)
		entries, problems := Parse(text)
		out = append(out, File{
			ID: strings.TrimSuffix(name, ".md"), Path: p, Text: text, Entries: entries, Problems: problems,
			Status: Status(entries), Empty: strings.TrimSpace(text) == "",
			ConvertedOnly: len(entries) == 1 && convertedRE.MatchString(entries[0].Title),
		})
	}
	return out
}

// FileMap is Files by id.
func FileMap(files []File) map[string]File {
	out := map[string]File{}
	for _, f := range files {
		out[f.ID] = f
	}
	return out
}

// PackRoots are where a file list reaches a pack: a canon's shelf and a
// member's local packs.
var PackRoots = []string{"packs", ".claudinite/local/packs"}

// PackDirsIn are the pack directories files reach under either root,
// sorted.
func PackDirsIn(files []string) []string {
	dirs := map[string]bool{}
	for _, f := range files {
		p := strings.ReplaceAll(f, "\\", "/")
		for _, root := range PackRoots {
			if !strings.HasPrefix(p, root+"/") {
				continue
			}
			rest := p[len(root)+1:]
			if cut := strings.Index(rest, "/"); cut > 0 {
				dirs[root+"/"+rest[:cut]] = true
			}
		}
	}
	return sortedKeys(dirs)
}
