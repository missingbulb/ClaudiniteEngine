package provenance

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const born = "## 2026-07-18 · born · a widget rule\n- **Actor:** @x (owner).\n- **Mechanism:** a RULES.md rule.\n"

func acmePack(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		".claudinite/local/packs/acme-pack/pack.json":                    "{}\n",
		".claudinite/local/packs/acme-pack/RULES.md":                     "- **Writing a widget** — keep it small. (writing-widget)\n",
		".claudinite/local/packs/acme-pack/provenance/_pack.md":          "",
		".claudinite/local/packs/acme-pack/provenance/writing-widget.md": "",
	}
	for k, v := range files {
		base[k] = v
	}
	for rel, text := range base {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func run(root, stdin string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Main(args, root, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestAppendRefusesASecretWhole(t *testing.T) {
	root := acmePack(t, nil)
	code, _, stderr := run(root, born+"- **Reason:** token ghp_"+strings.Repeat("a", 36)+"\n", "append", "acme-pack", "writing-widget")
	if code != 1 || !strings.Contains(stderr, "refused whole") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".claudinite/local/packs/acme-pack/provenance/writing-widget.md")); len(b) != 0 {
		t.Fatalf("a refused entry was written: %q", b)
	}
}

func TestAppendRefusesAnElementNoCarrierNames(t *testing.T) {
	root := acmePack(t, nil)
	code, _, stderr := run(root, born, "append", "acme-pack", "no-such")
	if code != 1 || !strings.Contains(stderr, "no-such.md does not exist") || strings.Contains(stderr, "--backfill batch") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = run(root, born, "append", "acme-pack", "no-such", "--backfill")
	if code != 0 {
		t.Fatalf("a backfill born opens the file: exit %d, stderr %q", code, stderr)
	}
}

func TestAppendRefusesAFirstEntryThatIsNotBorn(t *testing.T) {
	root := acmePack(t, nil)
	code, _, stderr := run(root, strings.Replace(born, "born", "reworded", 1), "append", "acme-pack", "writing-widget")
	if code != 1 || !strings.Contains(stderr, "writing-widget.md: ") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	code, stdout, _ := run(root, born, "append", "acme-pack", "writing-widget")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(stdout), "writing-widget.md: appended") {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}

func TestCheckExitsOneOnAFaultAndZeroOnPendingHistory(t *testing.T) {
	root := acmePack(t, nil)
	if code, stdout, _ := run(root, "", "check", "acme-pack"); code != 0 || !strings.Contains(stdout, "writing-widget.md ← rule \"Writing a widget\" (empty)") {
		t.Fatalf("pending history: exit %d, stdout %q", code, stdout)
	}
	root = acmePack(t, map[string]string{".claudinite/local/packs/acme-pack/RULES.md": "- **Writing a widget** — no marker.\n"})
	if code, stdout, _ := run(root, "", "check", "acme-pack"); code != 1 || !strings.Contains(stdout, "ends with no marker") {
		t.Fatalf("a fault: exit %d, stdout %q", code, stdout)
	}
}

func TestAnUnknownPackIsAUsageError(t *testing.T) {
	code, _, stderr := run(t.TempDir(), "", "check", "acme-pack")
	if code != 2 || !strings.Contains(stderr, `no pack "acme-pack"`) {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestAppendRefusesAMalformedEntry(t *testing.T) {
	filled := map[string]string{".claudinite/local/packs/acme-pack/provenance/writing-widget.md": born}
	for name, entry := range map[string]string{
		"unknown kind":            "## 2026-08-01 · polished · a widget rule\n- **Actor:** @x (owner).\n",
		"bad date":                "## 2026-13-45 · reworded · a widget rule\n- **Actor:** @x (owner).\n- **Mechanism:** a RULES.md rule.\n",
		"moved with no mechanism": "## 2026-08-01 · moved · a widget rule\n- **Actor:** @x (owner).\n",
	} {
		root := acmePack(t, filled)
		code, _, stderr := run(root, entry, "append", "acme-pack", "writing-widget")
		if code != 1 || stderr == "" {
			t.Errorf("%s: exit %d, stderr %q", name, code, stderr)
		}
		if b, _ := os.ReadFile(filepath.Join(root, ".claudinite/local/packs/acme-pack/provenance/writing-widget.md")); string(b) != born {
			t.Errorf("%s: the file changed: %q", name, b)
		}
	}
}

func TestTheVendoredMountIsNeverATarget(t *testing.T) {
	root := acmePack(t, map[string]string{
		".claudinite/shared/packs/acme-pack/pack.json": "{}\n",
		".claudinite/shared/packs/acme-pack/RULES.md":  "- **Writing a widget** — keep it small.\n",
	})
	for _, id := range []string{".claudinite/shared/packs/acme-pack", "./.claudinite/shared/packs/acme-pack/", ".claudinite/local/../shared/packs/acme-pack"} {
		code, _, stderr := run(root, "", "mark", id)
		if code != 2 || !strings.Contains(stderr, "vendored mount") {
			t.Fatalf("%s: exit %d, stderr %q", id, code, stderr)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".claudinite/shared/packs/acme-pack/RULES.md")); strings.Contains(string(b), "(writing-widget)") {
		t.Fatalf("the mount was marked: %q", b)
	}
	if code, _, stderr := run(root, "", "check", ".claudinite/local/packs/acme-pack"); code != 0 {
		t.Fatalf("a local pack by path: exit %d, stderr %q", code, stderr)
	}
}
