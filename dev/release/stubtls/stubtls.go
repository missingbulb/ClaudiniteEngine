// Package stubtls is the self-signed loopback certificate the release
// stand-ins (regstub, ghstub) serve, since every client they stand in for
// talks HTTPS only.
package stubtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"time"
)

// SelfSigned returns a day-long certificate for 127.0.0.1, ::1 and
// localhost, and its PEM for clients to trust.
func SelfSigned(name string) (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
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

// WriteReady writes base to path atomically, for a script waiting on it.
func WriteReady(path, base string) error {
	if err := os.WriteFile(path+".tmp", []byte(base+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// Config serves cert.
func Config(cert tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}
