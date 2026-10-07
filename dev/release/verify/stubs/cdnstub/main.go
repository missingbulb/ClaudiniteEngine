// Command cdnstub serves a vendored branch's objects at the pack CDN's
// paths, over HTTPS on loopback with a self-signed certificate, for the
// rehearsal's packs mode:
//
//	/packs/<id>/<file>  ->  <file> of <id>/ on the branch, read fresh per request
//	/packs/<file>       ->  catalog.json or catalog.sig.json at the branch's root
//
// --repo is the git repository (a bare one works) and --branch the branch,
// "vendored" by default. It writes its base URL to --ready once listening,
// the certificate to --ca-out (SSL_CERT_FILE for cn) and one line per
// request to --log. --down answers every request 503, as a CDN that is
// unreachable would leave cn to the branch.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/dev/release/verify/stubs/stubtls"
)

var (
	objectPath  = regexp.MustCompile(`^/packs/([a-z0-9][a-z0-9-]*)/([A-Za-z0-9][A-Za-z0-9._-]*)$`)
	catalogPath = regexp.MustCompile(`^/packs/(catalog\.json|catalog\.sig\.json)$`)
)

func main() {
	repo := flag.String("repo", "", "git repository holding the branch")
	branch := flag.String("branch", "vendored", "the vendored branch")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready", "", "file to write the base URL to once listening")
	caOut := flag.String("ca-out", "", "file to write the certificate PEM to")
	logPath := flag.String("log", "", "file to append one line per request to")
	down := flag.Bool("down", false, "answer every request 503")
	flag.Parse()
	if *repo == "" {
		fail(fmt.Errorf("--repo is required"))
	}
	cert, pemBytes, err := stubtls.SelfSigned("cdnstub")
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
		if *down {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		var rel string
		if m := objectPath.FindStringSubmatch(r.URL.EscapedPath()); m != nil {
			rel = m[1] + "/" + m[2]
		} else if m := catalogPath.FindStringSubmatch(r.URL.EscapedPath()); m != nil {
			rel = m[1]
		} else {
			http.NotFound(w, r)
			return
		}
		out, err := exec.Command("git", "--git-dir", *repo, "show", *branch+":"+rel).Output()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(out)
	})
	srv := &http.Server{Handler: h, TLSConfig: stubtls.Config(cert), ReadHeaderTimeout: 10 * time.Second}
	if *ready != "" {
		if err := stubtls.WriteReady(*ready, "https://"+ln.Addr().String()); err != nil {
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

func fail(err error) {
	fmt.Fprintf(os.Stderr, "cdnstub: %v\n", err)
	os.Exit(1)
}
