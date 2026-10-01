package adopt

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/verify"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const (
	pkg = "@claudinite/cli-rc"
	ver = "61001.1.0"
)

var (
	root   = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, 32))
	relKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, 32))
	now    = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

func tgz(t *testing.T, prefix string, files map[string][]byte) []byte {
	t.Helper()
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: prefix + n, Mode: 0o644, Size: int64(len(files[n])), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(files[n])
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// registry serves one signed engine release.
func registry(t *testing.T) npmreg.Client {
	t.Helper()
	files := map[string][]byte{}
	bin := []byte("#!/bin/sh\necho version " + ver + "\necho platform test\n")
	var lines []string
	name := strings.TrimPrefix(pkg, "@claudinite/")
	for _, p := range version.Platforms {
		data := []byte("binary for " + p)
		file := "cn"
		if strings.HasPrefix(p, "windows") {
			file = "cn.exe"
		}
		if p == version.Platform() {
			data = bin
		}
		sum := sha256.Sum256(data)
		lines = append(lines, fmt.Sprintf("    %q: {\"file\": %q, \"sha256\": %q, \"size\": %d}", p, file, hex.EncodeToString(sum[:]), len(data)))
		files[fmt.Sprintf("/%s-%s/-/%s-%s-%s.tgz", pkg, p, name, p, ver)] = tgz(t, "package/", map[string][]byte{"package.json": []byte("{}"), "bin/" + file: data})
	}
	manifest := []byte(fmt.Sprintf("{\n  \"v\": 1,\n  \"version\": %q,\n  \"binaries\": {\n%s\n  }\n}\n", ver, strings.Join(lines, ",\n")))
	cert, err := sign.Issue(root, relKey.Public().(ed25519.PublicKey), sign.UseManifest, now.AddDate(0, 0, -1), now.AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := json.Marshal(sign.SignManifest(relKey, cert, manifest))
	channel := tgz(t, "package/", map[string][]byte{"package.json": []byte("{}"), "manifest.json": manifest, "manifest.sig.json": sig, "launch": []byte("#!/bin/sh\n# the release's launcher\n")})
	path := fmt.Sprintf("/%s/-/%s-%s.tgz", pkg, name, ver)
	files[path] = channel
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.EscapedPath()]; ok {
			_, _ = w.Write(b)
			return
		}
		if strings.Replace(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "%2f", "/", 1) == pkg {
			s := sha512.Sum512(channel)
			p := npmreg.Packument{Name: pkg, Versions: map[string]npmreg.Version{}}
			var v npmreg.Version
			v.Version = ver
			v.Dist.Tarball, v.Dist.Integrity = srv.URL+path, "sha512-"+base64.StdEncoding.EncodeToString(s[:])
			p.Versions[ver] = v
			_ = json.NewEncoder(w).Encode(p)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return npmreg.Client{Registry: srv.URL, HTTP: srv.Client(), MaxBytes: npmreg.DefaultMaxBytes}
}

type fakePacks struct {
	t        *testing.T
	entries  map[string][]packindex.Entry
	archives map[string][]byte
}

func (f *fakePacks) publish(id, v, channel string, requires ...string) {
	a := tgz(f.t, "", map[string][]byte{"pack.json": []byte(`{"version": "` + v + `", "minEngineVersion": "` + ver + `"}`), "RULES.md": []byte("- " + id + "\n")})
	sum := sha256.Sum256(a)
	f.archives[id+"/"+v] = a
	f.entries[id] = append(f.entries[id], packindex.Entry{Version: v, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(a)), MinEngineVersion: ver, Channel: channel, Requires: requires})
}

func (f *fakePacks) VerifiedIndex(id string) (packs.Verified, error) {
	e, ok := f.entries[id]
	if !ok {
		return packs.Verified{}, fmt.Errorf("packs: %s: no source answered", id)
	}
	return packs.Verified{Index: packindex.Index{V: 1, Pack: id, Serial: 2, Versions: e}, From: "cdn"}, nil
}

func (f *fakePacks) Archive(id string, e packindex.Entry) ([]byte, error) {
	return f.archives[id+"/"+e.Version], nil
}

func newPacks(t *testing.T) *fakePacks {
	f := &fakePacks{t: t, entries: map[string][]packindex.Entry{}, archives: map[string][]byte{}}
	f.publish("hello", "1.0", "canary", "base")
	f.publish("base", "2.0", "stable")
	return f
}

func input(t *testing.T, repo string, ids ...string) (Input, *bytes.Buffer) {
	var out bytes.Buffer
	return Input{Repo: repo, Packs: ids, Channel: "canary", Package: pkg, Reader: newPacks(t), Timeout: 10 * time.Second, Out: &out,
		Fetch: update.FetchInput{Registry: registry(t), Roots: []ed25519.PublicKey{root.Public().(ed25519.PublicKey)},
			CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Platform: version.Platform(), Now: now}}, &out
}

func listFiles(t *testing.T, dir string) []string {
	var got []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(got)
	return got
}

func TestInitAdoptsAnEmptyRepo(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "hello")
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := []string{".claude/settings.json", ".claude/skills/.gitignore", ".claudinite/.gitignore",
		".claudinite/flat/claudinite-rules.GENERATED.md", ".claudinite/launch", ".claudinite/settings.yaml",
		".claudinite/shared/packs/base/RULES.md", ".claudinite/shared/packs/base/pack.json",
		".claudinite/shared/packs/hello/RULES.md", ".claudinite/shared/packs/hello/pack.json",
		".github/workflows/claudinite-ci.yml", ".github/workflows/claudinite-update.yml", "CLAUDE.md"}
	if got := listFiles(t, repo); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("files %v", got)
	}
	if idx, _ := os.ReadFile(filepath.Join(repo, ".claudinite/flat/claudinite-rules.GENERATED.md")); string(idx) != "@../shared/packs/base/RULES.md\n@../shared/packs/hello/RULES.md\n" {
		t.Errorf("index %q", idx)
	}
	if c, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md")); string(c) != "@.claudinite/flat/claudinite-rules.GENERATED.md\n" {
		t.Errorf("CLAUDE.md %q", c)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	e, err := settings.ReadEngine(raw, settings.YAML)
	if err != nil || e.Version != ver || e.Package != pkg {
		t.Errorf("pin %+v %v", e, err)
	}
	p, err := settings.ReadPacks(raw, settings.YAML)
	if err != nil || p.Channel != "canary" || strings.Join(p.Declared, ",") != "hello,base" {
		t.Errorf("packs %+v %v", p, err)
	}
	if l, _ := os.ReadFile(filepath.Join(repo, ".claudinite/launch")); string(l) != "#!/bin/sh\n# the release's launcher\n" {
		t.Errorf("launcher %q", l)
	}
	if g, _ := os.ReadFile(filepath.Join(repo, ".claude/skills/.gitignore")); string(g) != "*\n!.gitignore\n" {
		t.Errorf("skills ignore %q", g)
	}
	if w, _ := os.ReadFile(filepath.Join(repo, ".github/workflows/claudinite-update.yml")); !bytes.Equal(w, workflows.Templates()["claudinite-update.yml"]) {
		t.Error("update workflow is not the template")
	}
	if !strings.Contains(out.String(), "adding pack base, which hello 1.0 requires") || !strings.Contains(out.String(), "Commit everything above") {
		t.Errorf("output:\n%s", out)
	}
	// Launcher is the release's own here, so only that rule may speak.
	for _, f := range verify.Verify(verify.Input{Repo: repo, Launcher: []byte("#!/bin/sh\n# the release's launcher\n")}) {
		t.Errorf("verify: %s", f)
	}
}

func TestInitRefusesAnAdoptedRepo(t *testing.T) {
	for _, rel := range []string{".claudinite/settings.toml", ".claudinite/launch"} {
		repo := t.TempDir()
		_ = os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755)
		_ = os.WriteFile(filepath.Join(repo, filepath.FromSlash(rel)), []byte("x"), 0o644)
		in, _ := input(t, repo, "hello")
		if err := Init(in); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

func TestInitWritesNothingWhenAPackCannotBeRead(t *testing.T) {
	repo := t.TempDir()
	in, _ := input(t, repo, "hello", "nowhere")
	if err := Init(in); err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("%v", err)
	}
	in, _ = input(t, repo, "hello")
	in.Channel = "stable"
	if err := Init(in); err == nil || !strings.Contains(err.Error(), "canary") {
		t.Fatalf("a canary-only pack on stable: %v", err)
	}
	if got := listFiles(t, repo); len(got) != 0 {
		t.Errorf("wrote %v", got)
	}
}

func TestInitMergesAnExistingClaudeSettings(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".claude/settings.json"), []byte(`{"permissions": {"allow": ["Bash(ls)"]}, "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "echo mine"}]}]}}`), 0o644)
	in, out := input(t, repo, "base")
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var got struct {
		Permissions struct{ Allow []string }
		Hooks       map[string][]struct {
			Matcher string
			Hooks   []struct{ Command string }
		}
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claude/settings.json"))
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Permissions.Allow) != 1 || got.Hooks["Stop"][0].Hooks[0].Command != "echo mine" || got.Hooks["Stop"][1].Hooks[0].Command != ".claudinite/bin/cn hook stop" {
		t.Errorf("%s", raw)
	}
	if len(got.Hooks) != 6 || got.Hooks["PreToolUse"][0].Matcher != "*" {
		t.Errorf("%s", raw)
	}
	again, err := mergeHooks(filepath.Join(repo, ".claude/settings.json"))
	if err != nil || !bytes.Equal(again, raw) {
		t.Errorf("merging twice changed the file: %v", err)
	}
}

func TestAdoptDeclaresAndVendors(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755)
	body := "engine:\n  version: \"" + ver + "\"\n  manifest: \"sha512-" + strings.Repeat("A", 86) + "==\"\npacks:\n  channel: \"canary\"\n  declared:\n    - base\n"
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte(body), 0o644)
	var out bytes.Buffer
	if err := Adopt(AdoptInput{Repo: repo, ID: "hello", Reader: newPacks(t), Out: &out}); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	if string(raw) != body+"    - hello\n" {
		t.Errorf("%s", raw)
	}
	if got := listFiles(t, repo); strings.Join(got, " ") != ".claudinite/flat/claudinite-rules.GENERATED.md .claudinite/settings.yaml .claudinite/shared/packs/hello/RULES.md .claudinite/shared/packs/hello/pack.json" {
		t.Errorf("%v", got)
	}
	if err := Adopt(AdoptInput{Repo: repo, ID: "hello", Reader: newPacks(t), Out: &out}); err == nil || !strings.Contains(err.Error(), "already declared") {
		t.Errorf("%v", err)
	}
}
