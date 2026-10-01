// Package build is the checks binary: every declared pack's Go checks
// compiled, with the public check SDK, into one program in the cache,
// keyed by the engine version, the SDK and the sources, so it is rebuilt
// only when one of them changes.
//
// The build is offline (GOPROXY=off): the SDK's import path,
// claudinite.com/checksdk, resolves by a replace directive to the copy cn
// unpacks into <cache>/<version>/checksdk/, and a pack's checks/ folder is
// copied in as the package claudinite.checks/build/packs/<id> of one
// generated module, so no pack carries a go.mod. SessionStart starts it
// detached; the first hook that needs it waits. Local packs join the build
// with phase 6.
package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
)

// Config is where the build reads and writes.
type Config struct {
	// CacheRoot is the launcher's cache folder, .../claudinite.
	CacheRoot string
	// Engine is the running engine's version.
	Engine string
	// SDK maps each SDK file name to its content, as cn embeds it.
	SDK map[string][]byte
	// Go is the go command, "go" when empty.
	Go string
}

// Source is one Go file of a pack's checks.
type Source struct {
	Pack string
	// Path is relative to the pack's checks/ folder, with forward slashes.
	Path   string
	Data   []byte
	SHA256 string
}

// SDKModule is the import path packs use for the SDK.
const SDKModule = "claudinite.com/checksdk"

// module is the generated module's path; a pack's checks are its package
// module/packs/<id>, which the SDK reads to attribute each check.
const module = "claudinite.checks/build"

// staleLock is how old a build lock may be before another build takes it
// over.
const staleLock = 10 * time.Minute

// ErrBuilding is another process holding the key's build lock.
var ErrBuilding = errors.New("another process is building this checks binary")

func (c Config) checksRoot() string       { return filepath.Join(c.CacheRoot, "checks") }
func (c Config) lockPath(k string) string { return filepath.Join(c.checksRoot(), k+".lock") }

// Dir is the key's folder: the generated module, build.log and the binary.
func (c Config) Dir(key string) string { return filepath.Join(c.checksRoot(), key) }

// Binary is the key's checks binary.
func (c Config) Binary(key string) string {
	name := "checks"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(c.Dir(key), name)
}

func (c Config) failedPath(key string) string { return filepath.Join(c.Dir(key), "build.failed") }

func (c Config) goCmd() string {
	if c.Go == "" {
		return "go"
	}
	return c.Go
}

// Sources lists every .go file under each declared pack's checks/ folder,
// test files excepted, in a stable order.
func Sources(repo string, declared []string) ([]Source, error) {
	var out []Source
	for _, id := range declared {
		root := filepath.Join(packset.Tree(repo, id), "checks")
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			sum := sha256.Sum256(data)
			out = append(out, Source{Pack: id, Path: filepath.ToSlash(rel), Data: data, SHA256: hex.EncodeToString(sum[:])})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pack != out[j].Pack {
			return out[i].Pack < out[j].Pack
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func sdkHash(sdk map[string][]byte) string {
	var names []string
	for n := range sdk {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		sum := sha256.Sum256(sdk[n])
		fmt.Fprintf(h, "%s\x00%x\n", n, sum)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Key is the SHA-256 over the engine version, the SDK's hash and the
// sorted (pack, path, sha256) of every source; "" when there are none.
func Key(c Config, srcs []Source) string {
	if len(srcs) == 0 {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "engine\x00%s\x00sdk\x00%s\n", c.Engine, sdkHash(c.SDK))
	for _, s := range srcs {
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", s.Pack, s.Path, s.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// lock takes the key's build lock, a file created exclusively and holding
// the pid; one older than staleLock is taken over.
func lock(c Config, key string) (func(), error) {
	p := c.lockPath(key)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		st, serr := os.Stat(p)
		if serr != nil || time.Since(st.ModTime()) < staleLock {
			return nil, ErrBuilding
		}
		_ = os.Remove(p)
	}
	return nil, ErrBuilding
}

func locked(c Config, key string) bool {
	st, err := os.Stat(c.lockPath(key))
	return err == nil && time.Since(st.ModTime()) < staleLock
}

// UnpackSDK writes the SDK, read-only, as the module claudinite.com/checksdk
// in <cache>/<version>/checksdk/, rewriting it only when it differs.
func UnpackSDK(c Config) (string, error) {
	vdir := filepath.Join(c.CacheRoot, c.Engine)
	if err := paths.EnsurePrivateDir(c.CacheRoot); err != nil {
		return "", err
	}
	if err := paths.EnsurePrivateDir(vdir); err != nil {
		return "", err
	}
	dir := filepath.Join(vdir, "checksdk")
	files := map[string][]byte{"go.mod": []byte("module " + SDKModule + "\n\ngo 1.24\n")}
	for n, b := range c.SDK {
		files[n] = b
	}
	same := true
	for n, b := range files {
		have, err := os.ReadFile(filepath.Join(dir, n))
		same = same && err == nil && bytes.Equal(have, b)
	}
	if same {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for n, b := range files {
		if err := paths.PlaceReadOnly(dir, n, b, 0o444); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// buildEnv is the go command's whole environment: what it needs to find
// itself, its caches and a temp folder, never a token, and the settings
// that keep the build offline and reproducible.
func buildEnv() []string {
	var env []string
	for _, k := range []string{"PATH", "HOME", "XDG_CACHE_HOME", "TMPDIR", "TMP", "TEMP", "GOCACHE", "GOROOT", "GOPATH", "GOMODCACHE",
		"SYSTEMROOT", "USERPROFILE", "LOCALAPPDATA", "APPDATA"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return append(env, "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOWORK=off")
}

var goVersionRe = regexp.MustCompile(`\bgo1\.(\d+)`)

// goFloor refuses a go older than 1.24, from `go version`'s output.
func goFloor(out string) error {
	m := goVersionRe.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("cannot read the go version from %q", strings.TrimSpace(out))
	}
	if minor, _ := strconv.Atoi(m[1]); minor < 24 {
		return fmt.Errorf("%s is older than go1.24, which pack checks need", strings.TrimSpace(out))
	}
	return nil
}

// Build compiles srcs into the key's binary, unless it is already there.
// It writes the generated module and build.log (go version first, then
// the compiler's output) into the key's folder and places the binary 0555
// by rename. Another process building the same key makes it return
// ErrBuilding; a failure is recorded in build.log and build.failed.
func Build(c Config, key string, srcs []Source) error {
	if key == "" {
		return nil
	}
	if _, err := os.Stat(c.Binary(key)); err == nil {
		return nil
	}
	if err := paths.EnsurePrivateDir(c.CacheRoot); err != nil {
		return err
	}
	if err := paths.EnsurePrivateDir(c.checksRoot()); err != nil {
		return err
	}
	release, err := lock(c, key)
	if err != nil {
		return err
	}
	defer release()
	if _, err := os.Stat(c.Binary(key)); err == nil {
		return nil
	}
	dir := c.Dir(key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(c.failedPath(key))
	var log bytes.Buffer
	err = build(c, key, srcs, &log)
	if err != nil {
		fmt.Fprintf(&log, "\nbuild failed: %v\n", err)
	}
	if werr := os.WriteFile(filepath.Join(dir, "build.log"), log.Bytes(), 0o644); werr != nil && err == nil {
		err = werr
	}
	if err != nil {
		_ = os.WriteFile(c.failedPath(key), nil, 0o644)
	}
	return err
}

func build(c Config, key string, srcs []Source, log *bytes.Buffer) error {
	run := func(args ...string) error {
		cmd := exec.Command(c.goCmd(), args...)
		cmd.Dir = c.Dir(key)
		cmd.Env = buildEnv()
		cmd.Stdout, cmd.Stderr = log, log
		return cmd.Run()
	}
	if err := run("version"); err != nil {
		return fmt.Errorf("go version: %w (Go 1.24 or newer is needed to compile pack checks)", err)
	}
	if err := goFloor(log.String()); err != nil {
		return err
	}
	sdk, err := UnpackSDK(c)
	if err != nil {
		return fmt.Errorf("unpacking the SDK: %w", err)
	}
	dir := c.Dir(key)
	_ = os.RemoveAll(filepath.Join(dir, "packs"))
	goMod := fmt.Sprintf("module %s\n\ngo 1.24\n\nrequire %s v0.0.0\n\nreplace %s => %s\n", module, SDKModule, SDKModule, filepath.ToSlash(sdk))
	var imports []string
	seen := map[string]bool{}
	for _, s := range srcs {
		p := filepath.Join(dir, "packs", s.Pack, filepath.FromSlash(s.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, s.Data, 0o644); err != nil {
			return err
		}
		if !seen[s.Pack] {
			seen[s.Pack] = true
			imports = append(imports, fmt.Sprintf("\t_ %q\n", module+"/packs/"+s.Pack))
		}
	}
	mainGo := "// Code generated by cn check build. DO NOT EDIT.\n\npackage main\n\nimport (\n\t\"" + SDKModule + "\"\n\n" +
		strings.Join(imports, "") + ")\n\nfunc main() { checksdk.Main() }\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		return err
	}
	tmp := filepath.Base(c.Binary(key)) + ".tmp"
	_ = os.Remove(filepath.Join(dir, tmp))
	if err := run("build", "-trimpath", "-o", tmp, "."); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	if err := os.Chmod(filepath.Join(dir, tmp), 0o555); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, tmp), c.Binary(key))
}

// Start runs `<exe> check build --repo REPO --key KEY` detached, in its
// own session, and returns at once; the child outlives the hook.
func Start(exe, repo, key string) error {
	cmd := exec.Command(exe, "check", "build", "--repo", repo, "--key", key)
	cmd.SysProcAttr = detached()
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// ErrTimeout is Wait running out of time.
var ErrTimeout = errors.New("the checks binary was not ready in time")

// Wait polls for the key's binary until timeout. A build that failed with
// no other build under way ends the wait at once with that failure.
func Wait(c Config, key string, timeout time.Duration) (string, error) {
	start := time.Now()
	for {
		if _, err := os.Stat(c.Binary(key)); err == nil {
			return c.Binary(key), nil
		}
		if st, err := os.Stat(c.failedPath(key)); err == nil && !locked(c, key) &&
			(st.ModTime().After(start) || time.Since(start) > time.Second) {
			return "", fmt.Errorf("the checks build failed; see %s", filepath.Join(c.Dir(key), "build.log"))
		}
		if time.Since(start) > timeout {
			return "", ErrTimeout
		}
		time.Sleep(50 * time.Millisecond)
	}
}
