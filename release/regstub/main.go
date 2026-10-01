// Command regstub serves a release's tarballs at registry.npmjs.org's
// tarball URL shapes, over HTTPS on loopback with a self-signed
// certificate, for the launcher tests, the rehearsal and the timing probe:
//
//	/@claudinite/<name>/-/<name>-<version>.tgz  ->  <dist>/tarballs/<name>-<version>.tgz
//
// --dist may repeat; the first folder holding the tarball serves it.
//
// It writes its base URL to --ready once listening, the certificate to
// --ca-out (point curl at it with CURL_CA_BUNDLE), and one line per request
// to --log. --status N answers every request with N instead; --stall holds
// every connection open and never answers.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var tarballPath = regexp.MustCompile(`^/@claudinite(?:/|%2[fF])([a-z0-9-]+)/-/([a-z0-9.-]+\.tgz)$`)

func main() {
	var dists distList
	flag.Var(&dists, "dist", "release folder holding tarballs/ (repeatable; default dist)")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready", "", "file to write the base URL to once listening")
	caOut := flag.String("ca-out", "", "file to write the certificate PEM to")
	logPath := flag.String("log", "", "file to append one line per request to")
	status := flag.Int("status", 0, "answer every request with this status")
	stall := flag.Bool("stall", false, "hold every request open without answering")
	flag.Parse()
	if len(dists) == 0 {
		dists = distList{"dist"}
	}

	cert, pemBytes, err := selfSigned()
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
		m := tarballPath.FindStringSubmatch(r.URL.EscapedPath())
		if m == nil || !strings.HasPrefix(m[2], m[1]+"-") {
			http.NotFound(w, r)
			return
		}
		for _, d := range dists {
			p := filepath.Join(d, "tarballs", m[2])
			if _, err := os.Stat(p); err == nil {
				http.ServeFile(w, r, p)
				return
			}
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Handler: h, TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}}, ReadHeaderTimeout: 10 * time.Second}
	base := "https://" + ln.Addr().String()
	if *ready != "" {
		if err := os.WriteFile(*ready+".tmp", []byte(base+"\n"), 0o644); err != nil {
			fail(err)
		}
		if err := os.Rename(*ready+".tmp", *ready); err != nil {
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

type distList []string

func (d *distList) String() string     { return strings.Join(*d, ",") }
func (d *distList) Set(v string) error { *d = append(*d, v); return nil }

func selfSigned() (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "regstub"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	return c, certPEM, err
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "regstub: %v\n", err)
	os.Exit(1)
}
