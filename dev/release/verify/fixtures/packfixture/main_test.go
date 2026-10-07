package main

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/sign"
)

const src = "../../testdata/hello"

// packsKey writes a packs key and its certificate from the development
// root, as dev/release/verify/fixtures/packs-fixture.sh does with cn-keys.
func packsKey(t *testing.T) (string, string, []ed25519.PublicKey) {
	t.Helper()
	raw, err := os.ReadFile("../../../../keys/testkeys/root.key")
	if err != nil {
		t.Fatal(err)
	}
	root, err := sign.ParsePrivateKey(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	cert, err := sign.Issue(root, key.Public().(ed25519.PublicKey), sign.UsePacks, time.Now().Add(-time.Hour), time.Now().AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cb, _ := json.Marshal(cert)
	_ = os.WriteFile(filepath.Join(dir, "packs.key"), []byte(sign.FormatPrivateKey(key)), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "packs.cert.json"), cb, 0o644)
	return filepath.Join(dir, "packs.key"), filepath.Join(dir, "packs.cert.json"), []ed25519.PublicKey{root.Public().(ed25519.PublicKey)}
}

func verified(t *testing.T, tree string, roots []ed25519.PublicKey) packindex.Index {
	t.Helper()
	return verifiedPack(t, tree, "hello", roots)
}

func verifiedPack(t *testing.T, tree, pack string, roots []ed25519.PublicKey) packindex.Index {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(tree, pack, "index.json"))
	sigRaw, _ := os.ReadFile(filepath.Join(tree, pack, "index.sig.json"))
	var s sign.SignedPackIndex
	if err := json.Unmarshal(sigRaw, &s); err != nil {
		t.Fatal(err)
	}
	if _, err := sign.VerifyPackIndex(s, raw, roots, time.Now()); err != nil {
		t.Fatalf("the fixture's index does not verify: %v", err)
	}
	ix, err := packindex.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

// Another fixture pack publishes its folder as it is, at its own version
// and requires, beside hello's index; hello's labels are not its.
func TestAnotherPackPublishesItsSource(t *testing.T) {
	key, cert, roots := packsKey(t)
	tree := t.TempDir()
	args := []string{"--tree", tree, "--src", "../../testdata/hello-asks", "--key", key, "--cert", cert, "--min-engine", "1.61001.1", "--pack", "hello-asks", "--publish"}
	if err := run(append(args, "v2")); err == nil {
		t.Error("hello-asks published a hello label")
	}
	if err := run(append(args, "source")); err != nil {
		t.Fatal(err)
	}
	ix := verifiedPack(t, tree, "hello-asks", roots)
	if ix.Pack != "hello-asks" || len(ix.Versions) != 1 || ix.Versions[0].Version != "1.0" || strings.Join(ix.Versions[0].Requires, ",") != "hello" || ix.Versions[0].MinEngineVersion != "1.61001.1" {
		t.Fatalf("%+v", ix)
	}
	a, _ := os.ReadFile(filepath.Join(tree, "hello-asks", "1.0.tar.gz"))
	files, err := packs.ReadArchive(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"templates/hello-asks.md", "tasks/hello-asks-token/task.json", "RULES.md"} {
		if _, ok := files[n]; !ok {
			t.Errorf("the archive lacks %s", n)
		}
	}
}

func TestTheFixtureIndexVerifiesAndNamesItsArchives(t *testing.T) {
	key, cert, roots := packsKey(t)
	tree := t.TempDir()
	for _, l := range []string{"v1", "v2", "v3"} {
		if err := run([]string{"--tree", tree, "--src", src, "--key", key, "--cert", cert, "--min-engine", "1.61001.1", "--publish", l}); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"--tree", tree, "--key", key, "--cert", cert, "--revoke", "v3"}); err != nil {
		t.Fatal(err)
	}
	ix := verified(t, tree, roots)
	if ix.Serial != 4 || len(ix.Versions) != 3 || !ix.Versions[2].Revoked || ix.Versions[0].Channel != "canary" {
		t.Fatalf("%+v", ix)
	}
	for _, e := range ix.Versions {
		a, err := os.ReadFile(filepath.Join(tree, "hello", e.Version+".tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		if err := packs.VerifyArchive(a, e); err != nil {
			t.Errorf("%s: %v", e.Version, err)
		}
		files, err := packs.ReadArchive(a)
		if err != nil {
			t.Fatalf("%s: %v", e.Version, err)
		}
		if !strings.Contains(string(files["RULES.md"].Data), "# hello "+e.Version+"\n") {
			t.Errorf("%s: RULES.md %q", e.Version, files["RULES.md"].Data)
		}
		_, always := files["checks/always.go"]
		if always != (e.Version == "1.5") {
			t.Errorf("%s: always.go present %v", e.Version, always)
		}
		_, declared := files["declared-checks.json"]
		if declared != (e.Version != "1.0") {
			t.Errorf("%s: declared-checks.json present %v", e.Version, declared)
		}
		if changed := strings.Count(string(files["RULES.md"].Data), "The hello rule changed"); changed != map[bool]int{true: 0, false: 1}[e.Version == "1.0"] {
			t.Errorf("%s: the changed bullet appears %d times", e.Version, changed)
		}
		if guard := strings.Count(string(files["RULES.md"].Data), "The hello guard arrived"); guard != map[bool]int{true: 0, false: 1}[e.Version == "1.0"] {
			t.Errorf("%s: the guard bullet appears %d times", e.Version, guard)
		}
		if sdk := strings.Count(string(files["RULES.md"].Data), "The hello SDK probes arrived"); sdk != map[bool]int{true: 0, false: 1}[e.Version == "1.0"] {
			t.Errorf("%s: the SDK probes bullet appears %d times", e.Version, sdk)
		}
		_, fold := files["tasks/hello-fold/task.json"]
		_, rules := files["merge-rules.json"]
		actions := strings.Contains(string(files["pack.json"].Data), "githubActions")
		if fold != (e.Version != "1.0") || rules != (e.Version != "1.0") || actions != (e.Version != "1.0") {
			t.Errorf("%s: hello-fold %v, merge-rules.json %v, githubActions %v", e.Version, fold, rules, actions)
		}
		if tasks := strings.Count(string(files["RULES.md"].Data), "The hello tasks arrived"); tasks != map[bool]int{true: 0, false: 1}[e.Version == "1.0"] {
			t.Errorf("%s: the tasks bullet appears %d times", e.Version, tasks)
		}
		_, guide := files["skills/hello-guide/SKILL.md"]
		_, judge := files["checks/judge.go"]
		_, change := files["checks/change.go"]
		_, config := files["checks/config.go"]
		if guide != (e.Version != "1.0") || judge != (e.Version != "1.0") || change != (e.Version != "1.0") || config != (e.Version != "1.0") {
			t.Errorf("%s: hello-guide %v, judge.go %v, change.go %v, config.go %v", e.Version, guide, judge, change, config)
		}
	}
	if readme := string(mustVariant(t, "v1")["README.md"].Data); strings.Contains(readme, "Declared checks") || strings.Contains(readme, "Forced skill") || strings.Contains(readme, "**Judge**") || strings.Contains(readme, "SDK probes") || strings.Contains(readme, "**Tasks**") || !strings.Contains(readme, "**Skill**") {
		t.Errorf("v1 README:\n%s", readme)
	}
	// v2 is the source itself, as ClaudinitePacks publishes it, under
	// v2's version.
	source, _ := ReadPack(src)
	source["RULES.md"] = File{Data: []byte(rulesHeading.ReplaceAllString(string(source["RULES.md"].Data), "# hello "+Labels["v2"]))}
	for name, f := range mustVariant(t, "v2") {
		if name != "pack.json" && string(source[name].Data) != string(f.Data) {
			t.Errorf("v2 differs from the source in %s", name)
		}
	}
	again, _ := Archive(mustVariant(t, "v1"))
	first, _ := os.ReadFile(filepath.Join(tree, "hello", "1.0.tar.gz"))
	if string(again) != string(first) {
		t.Error("the archive is not deterministic")
	}
	if err := run([]string{"--tree", tree, "--key", key, "--cert", cert, "--serial", "1"}); err != nil {
		t.Fatal(err)
	}
	if ix := verified(t, tree, roots); ix.Serial != 1 {
		t.Errorf("serial %d", ix.Serial)
	}
	if err := run([]string{"--tree", tree, "--flip-sig"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(tree, "hello", "index.json"))
	sigRaw, _ := os.ReadFile(filepath.Join(tree, "hello", "index.sig.json"))
	var s sign.SignedPackIndex
	_ = json.Unmarshal(sigRaw, &s)
	if _, err := sign.VerifyPackIndex(s, raw, roots, time.Now()); err == nil {
		t.Error("a flipped signature still verifies")
	}
}

func mustVariant(t *testing.T, label string) map[string]File {
	t.Helper()
	files, err := ReadPack(src)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Variant(files, label, "1.61001.1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The fixture's source is the hello pack ClaudinitePacks publishes: when
// CLAUDINITE_PACKS_REPO names a vendored branch carrying hello, its
// archive at the source's version holds exactly the source's files.
func TestTheSourceIsThePublishedHelloPack(t *testing.T) {
	repo := os.Getenv("CLAUDINITE_PACKS_REPO")
	if repo == "" {
		t.Skip("CLAUDINITE_PACKS_REPO is not set")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", "--depth", "1", "--branch", packs.VendoredRef, repo, dir).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var pj struct{ Version string }
	raw, _ := os.ReadFile(filepath.Join(src, "pack.json"))
	_ = json.Unmarshal(raw, &pj)
	archive, err := os.ReadFile(filepath.Join(dir, "hello", pj.Version+".tar.gz"))
	if err != nil {
		t.Skipf("the vendored branch has no hello %s yet: %v", pj.Version, err)
	}
	have, err := ReadPack(src)
	if err != nil {
		t.Fatal(err)
	}
	conv := map[string]packs.File{}
	var names []string
	for n, f := range have {
		conv[n] = packs.File{Data: f.Data, Executable: f.Executable}
		names = append(names, n)
	}
	sort.Strings(names)
	diff, err := packs.FilesEqual(conv, archive)
	if err != nil || diff != "" {
		t.Errorf("dev/release/verify/testdata/hello (%v) is not the published hello %s: %v\n%s", names, pj.Version, err, diff)
	}
}

// ReadPack follows tools/vendor's rule: test/, docs/ and provenance/ at the
// root and the Go tests beside the checks stay out; everything else ships.
func TestReadPackDropsWhatVendoringDrops(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"pack.json", "checks/a.go", "checks/a_test.go", "test/x_test.go", "docs/d.md", "provenance/p.md", "skills/s/s_test.go"} {
		_ = os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, rel), []byte("x\n"), 0o644)
	}
	files, err := ReadPack(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	if got := strings.Join(names, " "); got != "checks/a.go pack.json skills/s/s_test.go" {
		t.Errorf("vendored set %q", got)
	}
}

// v5 needs an engine no rehearsal builds, and v6 names a Node engine
// version: the two floors the update must skip.
func TestTheUnreachableFloors(t *testing.T) {
	for label, want := range map[string]string{"v5": `"minEngineVersion": "99.991231.99"`, "v6": `"minEngineVersion": "60928.1"`, "v4": `"minEngineVersion": "1.61001.1"`} {
		if pj := string(mustVariant(t, label)["pack.json"].Data); !strings.Contains(pj, want) {
			t.Errorf("%s: %s", label, pj)
		}
	}
}
