// Command regstub serves a release's tarballs at registry.npmjs.org's
// tarball URL shapes, over HTTPS on loopback with a self-signed
// certificate, for the launcher tests, the rehearsal and the timing probe:
//
//	/@claudinite/<name>/-/<name>-<version>.tgz  ->  <dist>/tarballs/<name>-<version>.tgz
//	/@claudinite%2f<name> (or /@claudinite/<name>)  ->  a packument of every
//	                                                  <name>-<version>.tgz served
//	/v<version>/<name>-<version>.tgz            ->  <dist>/tarballs/<name>-<version>.tgz,
//	                                                the mirror's shape
//
// --dist may repeat; the first folder holding the tarball serves it.
// --deprecations names a JSON file {"<version>": "<message>"}, read on every
// packument request, whose messages become those versions' deprecated
// field, as npm deprecate would set them. Every packument's dist-tags carry
// latest on its newest version; --tag NAME (repeatable) points NAME there
// too, and --tag NAME=VERSION at VERSION, when the package has it.
//
// It writes its base URL to --ready once listening, the certificate to
// --ca-out (point curl at it with CURL_CA_BUNDLE, or on Windows import it
// into the Root store, since Schannel curl ignores that variable), and one
// line per request to --log. --status N answers every request with N
// instead; --stall holds every connection open and never answers.
package main

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/release/stubtls"
)

var (
	tarballPath   = regexp.MustCompile(`^/@claudinite(?:/|%2[fF])([a-z0-9-]+)/-/([a-z0-9.-]+\.tgz)$`)
	packumentPath = regexp.MustCompile(`^/@claudinite(?:/|%2[fF])([a-z0-9-]+)$`)
	mirrorPath    = regexp.MustCompile(`^/v([0-9.]+)/([a-z0-9.-]+\.tgz)$`)
	versionRe     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

func main() {
	var dists distList
	flag.Var(&dists, "dist", "release folder holding tarballs/ (repeatable; default dist)")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready", "", "file to write the base URL to once listening")
	caOut := flag.String("ca-out", "", "file to write the certificate PEM to")
	logPath := flag.String("log", "", "file to append one line per request to")
	status := flag.Int("status", 0, "answer every request with this status")
	stall := flag.Bool("stall", false, "hold every request open without answering")
	deprecations := flag.String("deprecations", "", "JSON file of version -> deprecation message")
	var tags tagList
	flag.Var(&tags, "tag", "dist-tag NAME on the newest version, or NAME=VERSION (repeatable)")
	flag.Parse()
	if len(dists) == 0 {
		dists = distList{"dist"}
	}

	cert, pemBytes, err := stubtls.SelfSigned("regstub")
	if err != nil {
		fail(err)
	}
	if *caOut != "" {
		if err := os.WriteFile(*caOut, pemBytes, 0o644); err != nil {
			fail(err)
		}
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fail(err)
	}
	var mu sync.Mutex
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if *logPath != "" {
			mu.Lock()
			if f, err := os.OpenFile(*logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintf(f, "%s %s\n", r.Method, r.URL.EscapedPath())
				_ = f.Close()
			}
			mu.Unlock()
		}
		if *stall {
			<-r.Context().Done()
			return
		}
		if *status != 0 {
			http.Error(w, http.StatusText(*status), *status)
			return
		}
		if pm := packumentPath.FindStringSubmatch(r.URL.EscapedPath()); pm != nil {
			servePackument(w, r, dists, pm[1], *deprecations, tags)
			return
		}
		file := ""
		if m := tarballPath.FindStringSubmatch(r.URL.EscapedPath()); m != nil && strings.HasPrefix(m[2], m[1]+"-") {
			file = m[2]
		} else if m := mirrorPath.FindStringSubmatch(r.URL.EscapedPath()); m != nil && strings.HasSuffix(m[2], "-"+m[1]+".tgz") {
			file = m[2]
		}
		if file == "" {
			http.NotFound(w, r)
			return
		}
		for _, d := range dists {
			p := filepath.Join(d, "tarballs", file)
			if _, err := os.Stat(p); err == nil {
				http.ServeFile(w, r, p)
				return
			}
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Handler: h, TLSConfig: stubtls.Config(cert), ReadHeaderTimeout: 10 * time.Second}
	base := "https://" + ln.Addr().String()
	if *ready != "" {
		if err := stubtls.WriteReady(*ready, base); err != nil {
			fail(err)
		}
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		_ = srv.Close()
	}()
	if err := srv.ServeTLS(ln, "", ""); err != nil && err != http.ErrServerClosed {
		fail(err)
	}
}

// servePackument answers npm's packument for @claudinite/<name>: every
// version some dist folder holds a <name>-<version>.tgz of, with the
// tarball's URL and SHA-512 integrity and any deprecation message, and the
// dist-tags.
func servePackument(w http.ResponseWriter, r *http.Request, dists distList, name, deprecationsFile string, tags tagList) {
	deprecated := map[string]string{}
	if deprecationsFile != "" {
		if raw, err := os.ReadFile(deprecationsFile); err == nil {
			if err := json.Unmarshal(raw, &deprecated); err != nil {
				http.Error(w, "deprecations file: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	type dist struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	}
	type ver struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		Deprecated string `json:"deprecated,omitempty"`
		Dist       dist   `json:"dist"`
	}
	versions := map[string]ver{}
	latest := ""
	for _, d := range dists {
		files, _ := filepath.Glob(filepath.Join(d, "tarballs", name+"-*.tgz"))
		for _, f := range files {
			v := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), name+"-"), ".tgz")
			if !versionRe.MatchString(v) {
				continue
			}
			if _, seen := versions[v]; seen {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			sum := sha512.Sum512(raw)
			base := "https://" + r.Host
			versions[v] = ver{Name: "@claudinite/" + name, Version: v, Deprecated: deprecated[v],
				Dist: dist{Tarball: base + "/@claudinite/" + name + "/-/" + filepath.Base(f), Integrity: "sha512-" + base64.StdEncoding.EncodeToString(sum[:])}}
			if latest == "" || newer(v, latest) {
				latest = v
			}
		}
	}
	if len(versions) == 0 {
		http.NotFound(w, r)
		return
	}
	distTags := map[string]string{"latest": latest}
	for _, t := range tags {
		tag, v, pinned := strings.Cut(t, "=")
		if !pinned {
			v = latest
		}
		if _, ok := versions[v]; ok {
			distTags[tag] = v
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"name": "@claudinite/" + name, "dist-tags": distTags, "versions": versions})
}

func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range pa {
		x, _ := strconv.ParseUint(pa[i], 10, 64)
		y, _ := strconv.ParseUint(pb[i], 10, 64)
		if x != y {
			return x > y
		}
	}
	return false
}

type distList []string

type tagList []string

func (t *tagList) String() string { return strings.Join(*t, ",") }
func (t *tagList) Set(v string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]*(=[0-9]+\.[0-9]+\.[0-9]+)?$`).MatchString(v) {
		return fmt.Errorf("--tag %q: want NAME or NAME=VERSION", v)
	}
	*t = append(*t, v)
	return nil
}

func (d *distList) String() string     { return strings.Join(*d, ",") }
func (d *distList) Set(v string) error { *d = append(*d, v); return nil }

func fail(err error) {
	fmt.Fprintf(os.Stderr, "regstub: %v\n", err)
	os.Exit(1)
}
