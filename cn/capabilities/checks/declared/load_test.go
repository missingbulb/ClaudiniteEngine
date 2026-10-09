package declared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/descriptor"
)

// The shelf's 22 declaration files, copied from the frozen Node engine
// checkout, load with no fault: 123 checks, 82 world, 8 work, 33 action.
func TestShelfLoads(t *testing.T) {
	var files []string
	_ = filepath.WalkDir("testdata/shelf", func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 22 {
		t.Fatalf("%d declaration files, want 22", len(files))
	}
	scopes := map[string]int{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		decls, err := Declarations(raw, descriptor.JSON)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var self *Regex
		if m := strings.Split(filepath.ToSlash(f), "/skills/"); len(m) == 2 {
			self = mustRegex(`(^|/)skills/`+escapeRe(strings.Split(m[1], "/")[0])+`/`, "")
		}
		for _, d := range decls {
			c, err := Compile(d, self)
			if err != nil {
				t.Errorf("%s: %v", f, err)
				continue
			}
			scopes[c.Kind()]++
			if u := UnplacedKeys(d); len(u) > 0 {
				t.Errorf("%s: %s has unplaced keys %v", f, c.ID, u)
			}
		}
	}
	if scopes["world"] != 82 || scopes["work"] != 8 || scopes["action"] != 33 {
		t.Errorf("scopes %v, want world 82, work 8, action 33", scopes)
	}
}

func compileOne(t *testing.T, decl string) (*Check, error) {
	t.Helper()
	decls, err := Declarations([]byte("["+decl+"]"), descriptor.JSON)
	if err != nil {
		t.Fatal(err)
	}
	return Compile(decls[0], nil)
}

func TestCompileFaults(t *testing.T) {
	for _, c := range []struct{ decl, want string }{
		{`{"on_fail":"block"}`, `needs a non-empty "id"`},
		{`{"id":"x","severity":"blocking"}`, `"on_fail": "block"`},
		{`{"id":"x"}`, `on_fail must be "block" or "advise"`},
		{`{"id":"x","on_fail":"advise","since":"2026-1-1"}`, `as YYYY-MM-DD`},
		{`{"id":"x","on_fail":"advise","scanFiles":"/a(/"}`, `is not a valid regex`},
		{`{"id":"x","on_fail":"advise","skipLinesMatching":"plain"}`, `takes a regex in /pattern/flags form`},
		{`{"id":"x","on_fail":"advise","skipLinesMatching":"/a/g"}`, `is not a valid regex`},
		{`{"id":"x","on_fail":"advise","scope":"world"}`, `"scope" takes "work"`},
		{`{"id":"x","on_fail":"advise","flagUntrackedFilesMatching":[{"match":"/a/"}]}`, `needs scope: "work"`},
		{`{"id":"x","on_fail":"advise","scope":"action"}`, `asserts nothing`},
		{`{"id":"x","on_fail":"advise","checkParsedFiles":[{"file":"a.json"}]}`, `asserts nothing`},
		{`{"id":"x","on_fail":"advise","extractValueSets":[{"setName":"s","fromTrackedPathsMatching":"/a/"}]}`, `"whenSetEmpty"`},
		{`{"id":"x","on_fail":"advise","requireIndexCoverage":[{"eachTrackedPathMatching":"/a/","indexFile":"i","coveredByText":"{path}","anchorFindingsAt":"indexFile"}]}`, `"whenIndexFileAbsent"`},
		{`{"id":"x","on_fail":"advise","scanFileClasses":["nope"]}`, `not a file class`},
		{`{"id":"x","on_fail":"advise","forbidReferences":[{"from":"a"}]}`, `needs a "to"`},
	} {
		_, err := compileOne(t, c.decl)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.decl, err, c.want)
		}
	}
}

func TestUnplacedDroppedAndReported(t *testing.T) {
	c, err := compileOne(t, `{"id":"x","on_fail":"advise","scanFiles":"/a/","descripton":"typo","matchLines":[{"match":"/b/","wat":"oops"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Spec["descripton"]; ok {
		t.Error("an unplaced key stays in the compiled spec")
	}
	decls, _ := Declarations([]byte(`[{"id":"x","on_fail":"advise","descripton":"typo","matchLines":[{"match":"/b/","wat":"oops"}]}]`), descriptor.JSON)
	u := UnplacedKeys(decls[0])
	if len(u) != 2 || u[0].Key != "descripton" || u[0].Container != "spec" || u[1].Key != "wat" || u[1].Container != "matchLines" {
		t.Errorf("unplaced %+v", u)
	}
}

func TestDeclarationFormats(t *testing.T) {
	want := `[{"id":"a","on_fail":"block","scanFiles":"/x/","matchLines":[{"match":"/y/i","what":"w"}]}]`
	yml := "- id: a\n  on_fail: block\n  scanFiles: /x/\n  matchLines:\n    - match: /y/i\n      what: w\n"
	toml := "[[check]]\nid = \"a\"\non_fail = \"block\"\nscanFiles = \"/x/\"\n[[check.matchLines]]\nmatch = \"/y/i\"\nwhat = \"w\"\n"
	j, err := Declarations([]byte(want), descriptor.JSON)
	if err != nil {
		t.Fatal(err)
	}
	for f, raw := range map[descriptor.Format]string{descriptor.YAML: yml, descriptor.TOML: toml} {
		d, err := Declarations([]byte(raw), f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if jsString(d[0]["id"]) != "a" || len(items(d[0]["matchLines"])) != 1 || jsString(items(d[0]["matchLines"])[0]["match"]) != "/y/i" || len(d) != len(j) {
			t.Errorf("%s: %v", f, d)
		}
	}
	if _, err := Declarations([]byte("other = 1\n"), descriptor.TOML); err == nil {
		t.Error("a TOML declaration with another root key loads")
	}
}

// The vocabulary tables are the Node engine's, read from the frozen
// checkout when one is named.
func TestVocabularyDrift(t *testing.T) {
	root := os.Getenv("CLAUDINITE_NODE_ENGINE")
	if root == "" {
		t.Skip("CLAUDINITE_NODE_ENGINE is unset; the vocabulary drift test needs the Node engine checkout")
	}
	raw, err := os.ReadFile(filepath.Join(root, "engine/checks/helpers/pattern-rules.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for container, keys := range specKeys {
		for _, k := range keys {
			if !strings.Contains(src, "'"+k+"'") {
				t.Errorf("SPEC_KEYS.%s: %q is not in the Node table", container, k)
			}
		}
	}
	start := strings.Index(src, "const SPEC_KEYS = {")
	end := strings.Index(src[start:], "\n};") + start
	table := src[start:end]
	n := 0
	for _, l := range strings.Split(table, "\n") {
		if i := strings.Index(l, ": ["); i > 0 && !strings.HasPrefix(strings.TrimSpace(l), "//") {
			key := strings.TrimSpace(l[:i])
			if _, ok := specKeys[key]; !ok {
				t.Errorf("the Node table has container %q, this one does not", key)
			}
			n++
		} else if strings.Contains(l, ": MSG,") {
			key := strings.TrimSpace(l[:strings.Index(l, ":")])
			if _, ok := specKeys[key]; !ok {
				t.Errorf("the Node table has container %q, this one does not", key)
			}
			n++
		}
	}
	if n+1 != len(specKeys) && n != len(specKeys) {
		t.Errorf("the Node table has %d containers, this one %d", n, len(specKeys))
	}
	for name, src2 := range fileClassSources {
		if !strings.Contains(src, name+": /"+src2+"/") {
			t.Errorf("FILE_CLASSES.%s is not /%s/ in the Node engine", name, src2)
		}
	}
}
