package rulesindex

import (
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// nodeOrder is the shelf's skill names and a few edge cases as Node's
// localeCompare sorted them.
var nodeOrder = []string{
	"a b",
	"a_b",
	"a-1",
	"a-b",
	"A-b",
	"a.b",
	"a1",
	"ab",
	"adopt-claudinite",
	"adopt-pack",
	"alpha",
	"authoring-agent-docs",
	"backfilling-provenance",
	"bug-investigation",
	"changing-pack-elements",
	"chrome-store-releases",
	"ci-performance-evaluation",
	"committing",
	"configuring-the-fleet",
	"create-release-plan",
	"deterministic-expecteds",
	"do-later",
	"explore-link",
	"extension-host-permissions",
	"extract-from-activity",
	"extract-from-conversations",
	"extract-from-instructions",
	"extract-packs-from-a-project",
	"fetching-from-the-web",
	"file-placement",
	"firebase-functions",
	"firestore-security-rules",
	"flutter-golden-tests",
	"flutter-pubspec",
	"git-github-advanced",
	"github-actions-scheduling",
	"github-pages-pipeline",
	"google-id-token-validation",
	"growth-dedup",
	"hello",
	"hello-guide",
	"improve-comments",
	"jwt-minting",
	"jwt-validation",
	"learning-a-technology",
	"macos-app-bundle",
	"macos-entitlements-and-tcc",
	"map-a-data-source",
	"merge-to-main",
	"node-test-discovery",
	"production-retrospective",
	"prose-to-checks",
	"python-optional-deps",
	"releasing-a-cloudflare-site",
	"repo-text-sweeps",
	"revalidating-rules",
	"sam-build-and-deps",
	"sam-template",
	"searching-for-a-tool",
	"triaging-usage-findings",
	"unattended-agents",
	"verify-in-production",
	"web-speech-io",
	"working-with-generated-files",
	"write-a-requirement-leaf",
	"write-a-saga",
	"writing-claudinite-skills",
	"writing-handover-issues",
	"writing-migration-plans",
	"writing-pack-prose",
	"writing-repo-scanning-checks",
	"writing-tasks",
	"writing-tests",
	"writing-wiki-pages",
	"Zeta",
}

func TestLocaleLessMatchesNode(t *testing.T) {
	got := append([]string{}, nodeOrder...)
	r := rand.New(rand.NewSource(1))
	r.Shuffle(len(got), func(i, j int) { got[i], got[j] = got[j], got[i] })
	sort.SliceStable(got, func(i, j int) bool { return localeLess(got[i], got[j]) })
	for i := range got {
		if got[i] != nodeOrder[i] {
			t.Fatalf("at %d got %q, Node %q\n%v", i, got[i], nodeOrder[i], got)
		}
	}
}

// Converge writes the skills index with the rules index, a shared name
// resolving to canon's, and removes it once no skill is mounted.
func TestConvergeWritesAndRemovesTheSkillsIndex(t *testing.T) {
	repo := member(t)
	write(t, filepath.Join(repo, ".claudinite/shared/packs/web/pack.json"), `{"version": "1.0", "skills": ["shared"]}`)
	write(t, filepath.Join(repo, ".claudinite/shared/packs/web/skills/shared/SKILL.md"), "---\nname: shared\ndescription: canon's | own\n---\n")
	write(t, filepath.Join(repo, ".claudinite/local/packs/mine/pack.json"), `{"skills": ["shared"]}`)
	write(t, filepath.Join(repo, ".claudinite/local/packs/mine/skills/shared/SKILL.md"), "---\nname: shared\ndescription: local\n---\n")
	got, err := Converge(repo, "0.0.0")
	if err != nil || !slices.Contains(got, SkillsFile) {
		t.Fatalf("%v %v", got, err)
	}
	b, _ := os.ReadFile(filepath.Join(repo, SkillsFile))
	if !strings.Contains(string(b), "| `shared` | web | canon's \\| own |\n") || strings.Contains(string(b), "local") {
		t.Errorf("index:\n%s", b)
	}
	if again, _ := Converge(repo, "0.0.0"); slices.Contains(again, SkillsFile) {
		t.Error("a current index rewritten")
	}
	_ = os.RemoveAll(filepath.Join(repo, ".claudinite/shared/packs/web/skills"))
	_ = os.RemoveAll(filepath.Join(repo, ".claudinite/local/packs/mine/skills"))
	if _, err := Converge(repo, "0.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, SkillsFile)); !os.IsNotExist(err) {
		t.Errorf("a stale skills index survived: %v", err)
	}
}
