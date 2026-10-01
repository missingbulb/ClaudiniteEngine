package skillfm

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// Cases read by the Node engine's parseFrontmatter at the frozen commit; the
// expected values are what that function returns for the same text.
func TestParseMatchesNodeSubset(t *testing.T) {
	cases := []struct {
		name string
		text string
		want map[string]any
	}{
		{"none", "# no frontmatter\n", map[string]any{}},
		{"unterminated", "---\nname: x\n", map[string]any{}},
		{"scalars and quotes", "---\nname: do-later\ndescription: \"Queue it: later\"\nother: 'single'\n---\nbody", map[string]any{"name": "do-later", "description": "Queue it: later", "other": "single"}},
		{"nested map and lists", "---\nname: s\nmetadata:\n  body: workflow\n  force-load-on-file-edits-paths:\n    - \"**/*.go\"\n    - docs/\n  force-load-on-prompts-matching: [/deploy/i, \"/ship/\"]\n---\n",
			map[string]any{"name": "s", "metadata": map[string]any{"body": "workflow", "force-load-on-file-edits-paths": []any{"**/*.go", "docs/"}, "force-load-on-prompts-matching": []any{"/deploy/i", "/ship/"}}}},
		{"empty key stays an empty list", "---\nlist:\nnext: 1\n---\n", map[string]any{"list": []any{}, "next": "1"}},
		{"item without an open key is skipped", "---\n  - stray\nname: n\n---\n", map[string]any{"name": "n"}},
		{"crlf lines carry no keys", "---\r\nname: n\r\n---\r\n", map[string]any{}},
		{"item under a map is dropped", "---\nmetadata:\n  a: 1\n  - x\n---\n", map[string]any{"metadata": map[string]any{"a": "1"}}},
	}
	for _, c := range cases {
		if got := Parse(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %#v\nwant %#v", c.name, got, c.want)
		}
	}
}

func TestRead(t *testing.T) {
	m := Read("---\nname: s\ndescription: d\nmetadata:\n  body: nonsense\n  force-load-on-file-edits-paths: a/**, b/*.md\n  force-load-on-tool-calls: Bash /git push/\n  force-load-on-tool-results-matching:\n    - /error/\n---\n")
	want := Meta{Name: "s", Description: "d", ForceLoadPaths: []string{"a/**", "b/*.md"}, ToolCalls: []string{"Bash /git push/"}, ToolResults: []string{"/error/"}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("%+v", m)
	}
	if Read("---\nmetadata:\n  body: guidelines\n---\n").Body != "guidelines" {
		t.Error("body")
	}
}

// Every SKILL.md on the frozen shelf parses to what the Node engine's
// reader returns, when that checkout is at hand.
func TestShelfMatchesNode(t *testing.T) {
	root := os.Getenv("CLAUDINITE_NODE_ENGINE")
	if root == "" {
		t.Skip("CLAUDINITE_NODE_ENGINE is not set")
	}
	files, _ := filepath.Glob(filepath.Join(root, "packs", "*", "skills", "*", "SKILL.md"))
	local, _ := filepath.Glob(filepath.Join(root, ".claudinite", "local", "packs", "*", "skills", "*", "SKILL.md"))
	files = append(files, local...)
	if len(files) == 0 {
		t.Fatal("no SKILL.md under the checkout")
	}
	script := `import { parseFrontmatter } from ` + strconv.Quote(filepath.ToSlash(filepath.Join(root, "engine/pack_loader/skill-frontmatter.mjs"))) + `;
import { readFileSync } from "node:fs";
const out = {};
for (const f of process.argv.slice(1)) out[f] = parseFrontmatter(readFileSync(f, "utf8"));
process.stdout.write(JSON.stringify(out));`
	cmd := exec.Command("node", append([]string{"--input-type=module", "-e", script}, files...)...)
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		body, _ := os.ReadFile(f)
		got := Parse(string(body))
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want[f])
		if string(g) != string(w) {
			t.Errorf("%s:\n go   %s\n node %s", f, g, w)
		}
	}
	t.Logf("%d SKILL.md files agree", len(files))
}
