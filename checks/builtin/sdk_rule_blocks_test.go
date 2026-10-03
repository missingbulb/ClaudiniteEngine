package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/checksdk"
	prov "github.com/missingbulb/ClaudiniteEngine/shared/provenance"
)

// checksdk.RuleBlocks is shared/provenance's RuleBlocks (bold bullets only) as
// a pack check reads it from the SDK's pure half, which cannot import the
// engine: the two agree on every block's lines, marker and trigger, over
// fixtures selecting each branch and every prose file of the frozen
// shelf and the packs tree the environment names.
func TestSDKRuleBlocksAgree(t *testing.T) {
	texts := []string{
		"# x\n\n- **Lead** body. (lead-in-slug)\n- plain bullet\n- **Two**\n  more\n\n  nested para (two-slug)\n",
		"- **A** text (12)\n- **B** `code` — dash –\n  1. item\n  (b-slug)\n```\n- **fenced** (no-slug)\n```\n## h\n- **C**\n",
		"- **Open fence**\n```\n- **in** (x-y)\n",
		"- no bold, first clause: rest\n- **x**y** (q-r) \n\n\n",
		"",
	}
	for _, env := range []string{"CLAUDINITE_NODE_ENGINE", "CLAUDINITE_PACKS_TREE"} {
		root := os.Getenv(env)
		if root == "" {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(root, "packs", "*", "RULES.md"))
		more, _ := filepath.Glob(filepath.Join(root, "packs", "*", "skills", "*", "SKILL.md"))
		for _, f := range append(files, more...) {
			raw, err := os.ReadFile(f)
			if err == nil {
				texts = append(texts, string(raw))
			}
		}
	}
	for _, text := range texts {
		want := prov.RuleBlocks(text, false)
		got := checksdk.RuleBlocks(text)
		if len(got) != len(want) {
			t.Errorf("%d blocks, the engine reads %d:\n%s", len(got), len(want), firstLine(text))
			continue
		}
		for i, w := range want {
			g := got[i]
			if g.Start != w.Start || g.End != w.End || g.LastLine != w.LastLine || g.Slug != w.Slug || g.Trigger != w.Trigger {
				t.Errorf("block %d: sdk %+v, engine %+v\n%s", i, g, w, firstLine(text))
			}
		}
	}
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }
