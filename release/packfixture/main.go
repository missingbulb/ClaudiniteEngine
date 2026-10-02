// Command packfixture writes the rehearsal's local pack source into a
// checkout of a vendored branch, laid out as ClaudinitePacks' is:
// <id>/<version>.tar.gz and <id>/index.json with index.sig.json beside
// it, signed for use packs. release/packs-fixture.sh drives it and
// commits the result.
//
//	packfixture --tree DIR --src DIR --key K --cert C --min-engine V --publish vN
//	packfixture --tree DIR --key K --cert C --revoke vN
//	packfixture --tree DIR --key K --cert C --serial N
//	packfixture --tree DIR --flip-sig
//
// The labels name the hello pack's rehearsal versions: v2 (1.2) is the
// source as it is, which ClaudinitePacks publishes; v1 is 1.0, the source
// without its 1.1 and 1.2 rule bullets, its declared checks, its forced
// skill and its judge; v3 (1.3) adds a check that finds on every repo; v4
// (1.4) drops it again; v5 (1.5) needs an engine no rehearsal builds. Every publish and revoke bumps the serial; --serial
// rewrites it, as an index that regressed would read.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// Pack is the fixture's one pack.
const Pack = "hello"

// Labels maps a rehearsal label to its pack version.
var Labels = map[string]string{"v1": "1.0", "v2": "1.2", "v3": "1.3", "v4": "1.4", "v5": "1.5"}

// unreachableEngine is v5's minEngineVersion.
const unreachableEngine = "99999.0.0"

// dropped are the folders tools/vendor leaves out at a pack's root.
var dropped = map[string]bool{"test": true, "docs": true, "provenance": true}

// File is one file of a pack's vendored set.
type File struct {
	Data       []byte
	Executable bool
}

// ReadPack reads a pack folder's vendored set by tools/vendor's rule.
func ReadPack(dir string) (map[string]File, error) {
	out := map[string]File{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if !strings.Contains(rel, "/") && dropped[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = File{Data: data, Executable: info.Mode().Perm()&0o111 != 0}
		return nil
	})
	return out, err
}

// Archive packs files as tools/vendor does: ustar, sorted by path bytes,
// regular files with mode 0644 or 0755, mtime 0, uid and gid 0, and a gzip
// header with no name, no time and an unknown OS byte.
func Archive(files map[string]File) ([]byte, error) {
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, n := range names {
		mode := int64(0o644)
		if files[n].Executable {
			mode = 0o755
		}
		h := &tar.Header{Name: n, Mode: mode, Size: int64(len(files[n].Data)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(h); err != nil {
			return nil, err
		}
		if _, err := tw.Write(files[n].Data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	gz, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	gz.OS = 0xff
	if _, err := gz.Write(raw.Bytes()); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

var (
	versionField = regexp.MustCompile(`"version": "[^"]*"`)
	minField     = regexp.MustCompile(`"minEngineVersion": "[^"]*"`)
	rulesHeading = regexp.MustCompile(`(?m)^# hello .*$`)
	changedRule  = "- **The hello rule changed** — this bullet arrived with hello 1.1.\n"
	guardRule    = regexp.MustCompile("(?m)^- \\*\\*The hello guard arrived\\*\\*.*\n")
	declaredLine = regexp.MustCompile("(?m)^- \\*\\*(Declared checks|Forced skill|Judge)\\*\\*(.*\n)(  .*\n)*")
)

// declaredChecks is the descriptor hello 1.1 added.
const declaredChecks = "declared-checks.json"

// Variant is the hello pack's files at label, built from the source.
func Variant(src map[string]File, label, minEngine string) (map[string]File, error) {
	ver, ok := Labels[label]
	if !ok {
		return nil, fmt.Errorf("unknown label %q (v1..v5)", label)
	}
	out := map[string]File{}
	for n, f := range src {
		out[n] = f
	}
	if label == "v5" {
		minEngine = unreachableEngine
	}
	pj := string(out["pack.json"].Data)
	pj = versionField.ReplaceAllString(pj, `"version": "`+ver+`"`)
	pj = minField.ReplaceAllString(pj, `"minEngineVersion": "`+minEngine+`"`)
	out["pack.json"] = File{Data: []byte(pj)}
	rules := rulesHeading.ReplaceAllString(string(out["RULES.md"].Data), "# hello "+ver)
	if label == "v1" {
		rules = strings.Replace(rules, changedRule, "", 1)
		rules = guardRule.ReplaceAllString(rules, "")
		delete(out, declaredChecks)
		delete(out, "skills/hello-guide/SKILL.md")
		delete(out, "checks/judge.go")
		out["README.md"] = File{Data: declaredLine.ReplaceAll(out["README.md"].Data, nil)}
	}
	out["RULES.md"] = File{Data: []byte(rules)}
	if label == "v3" {
		out["checks/always.go"] = File{Data: []byte(alwaysCheck)}
	}
	return out, nil
}

const alwaysCheck = `package checks

import "claudinite.com/checksdk"

func init() {
	checksdk.Register(checksdk.Check{
		ID:   "always",
		Tags: []string{"work", "world"},
		Run: func(checksdk.Repo) []checksdk.Finding {
			return []checksdk.Finding{{Class: checksdk.ClassFinding, Path: ".", Sentence: "hello 1.3 finds on every repo"}}
		},
	})
}
`

// Entry is one index entry, in docs/release.md's key order.
type Entry struct {
	Version          string   `json:"version"`
	SHA256           string   `json:"sha256"`
	Size             int      `json:"size"`
	MinEngineVersion string   `json:"minEngineVersion"`
	Requires         []string `json:"requires"`
	Channel          string   `json:"channel"`
	Revoked          bool     `json:"revoked"`
	PublishedAt      string   `json:"publishedAt"`
	SourceCommit     string   `json:"sourceCommit"`
}

// Index is a pack's index.json.
type Index struct {
	V        int     `json:"v"`
	Pack     string  `json:"pack"`
	Serial   int     `json:"serial"`
	Versions []Entry `json:"versions"`
}

func readIndex(tree string) (Index, error) {
	raw, err := os.ReadFile(filepath.Join(tree, Pack, "index.json"))
	if os.IsNotExist(err) {
		return Index{V: 1, Pack: Pack}, nil
	}
	if err != nil {
		return Index{}, err
	}
	var ix Index
	return ix, json.Unmarshal(raw, &ix)
}

// writeIndex writes index.json as ClaudinitePacks serializes it and signs
// it.
func writeIndex(tree string, ix Index, key, cert string) error {
	raw, err := json.MarshalIndent(ix, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	k, err := os.ReadFile(key)
	if err != nil {
		return err
	}
	priv, err := sign.ParsePrivateKey(string(k))
	if err != nil {
		return err
	}
	cb, err := os.ReadFile(cert)
	if err != nil {
		return err
	}
	var c sign.Certificate
	if err := json.Unmarshal(cb, &c); err != nil {
		return err
	}
	sig, err := json.MarshalIndent(sign.SignPackIndex(priv, c, raw), "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(tree, Pack)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), raw, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.sig.json"), append(sig, '\n'), 0o644)
}

func publish(tree, src, label, minEngine, key, cert string) error {
	files, err := ReadPack(src)
	if err != nil {
		return err
	}
	files, err = Variant(files, label, minEngine)
	if err != nil {
		return err
	}
	archive, err := Archive(files)
	if err != nil {
		return err
	}
	ix, err := readIndex(tree)
	if err != nil {
		return err
	}
	ver := Labels[label]
	for _, e := range ix.Versions {
		if e.Version == ver {
			return fmt.Errorf("%s %s is already published", Pack, ver)
		}
	}
	if err := os.MkdirAll(filepath.Join(tree, Pack), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tree, Pack, ver+".tar.gz"), archive, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	var pj struct {
		MinEngineVersion string `json:"minEngineVersion"`
	}
	_ = json.Unmarshal(files["pack.json"].Data, &pj)
	ix.Serial++
	ix.Versions = append(ix.Versions, Entry{Version: ver, SHA256: hex.EncodeToString(sum[:]), Size: len(archive),
		MinEngineVersion: pj.MinEngineVersion, Requires: []string{}, Channel: "canary",
		PublishedAt: "2026-10-01T00:00:00Z", SourceCommit: strings.Repeat("0", 40)})
	return writeIndex(tree, ix, key, cert)
}

func run(args []string) error {
	fs := flag.NewFlagSet("packfixture", flag.ContinueOnError)
	tree := fs.String("tree", "", "")
	src := fs.String("src", "", "")
	key := fs.String("key", "", "")
	cert := fs.String("cert", "", "")
	minEngine := fs.String("min-engine", "", "")
	pub := fs.String("publish", "", "")
	revoke := fs.String("revoke", "", "")
	serial := fs.Int("serial", 0, "")
	flip := fs.Bool("flip-sig", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tree == "" {
		return fmt.Errorf("--tree is required")
	}
	switch {
	case *pub != "":
		if *src == "" || *minEngine == "" || *key == "" || *cert == "" {
			return fmt.Errorf("--publish needs --src, --min-engine, --key and --cert")
		}
		return publish(*tree, *src, *pub, *minEngine, *key, *cert)
	case *revoke != "":
		ix, err := readIndex(*tree)
		if err != nil {
			return err
		}
		found := false
		for i := range ix.Versions {
			if ix.Versions[i].Version == Labels[*revoke] {
				ix.Versions[i].Revoked, found = true, true
			}
		}
		if !found {
			return fmt.Errorf("%s is not published", *revoke)
		}
		ix.Serial++
		return writeIndex(*tree, ix, *key, *cert)
	case *serial > 0:
		ix, err := readIndex(*tree)
		if err != nil {
			return err
		}
		ix.Serial = *serial
		return writeIndex(*tree, ix, *key, *cert)
	case *flip:
		p := filepath.Join(*tree, Pack, "index.sig.json")
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		i := bytes.Index(raw, []byte(`"signature": "`))
		if i < 0 {
			return fmt.Errorf("%s holds no signature", p)
		}
		i += len(`"signature": "`)
		if raw[i] == 'A' {
			raw[i] = 'B'
		} else {
			raw[i] = 'A'
		}
		return os.WriteFile(p, raw, 0o644)
	}
	return fmt.Errorf("say --publish, --revoke, --serial or --flip-sig")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "packfixture: %v\n", err)
		os.Exit(1)
	}
}
