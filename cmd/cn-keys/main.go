// Command cn-keys is the offline key ceremony tool: it makes root and
// subject keys, certifies a subject key for a use, and verifies
// certificates. It is built locally and never published; README.md is the
// ceremony runbook.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

const usage = `usage:
  cn-keys root new --out DIR [--name root]
  cn-keys key new --out DIR --name NAME
  cn-keys certify --root ROOT.key --subject SUBJECT.pub --use USE --days N --out CERT.json
  cn-keys verify --roots DIR CERT.json
  cn-keys keyid KEY.pub

uses: manifest (up to 365 days), packs, license, license-public (up to 90 days)
`

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	err := dispatch(args, stdout)
	if err == nil {
		return 0
	}
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "cn-keys: %s\n%s", ue.msg, usage)
		return 2
	}
	fmt.Fprintf(stderr, "cn-keys: %v\n", err)
	return 1
}

func dispatch(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageError{"no command"}
	}
	switch {
	case len(args) >= 2 && args[0] == "root" && args[1] == "new":
		return makeKey(args[2:], "root", stdout)
	case len(args) >= 2 && args[0] == "key" && args[1] == "new":
		return makeKey(args[2:], "", stdout)
	case args[0] == "certify":
		return certify(args[1:], stdout)
	case args[0] == "verify":
		return verify(args[1:], stdout)
	case args[0] == "keyid":
		if len(args) != 2 {
			return usageError{"keyid takes one public key file"}
		}
		p, err := readPublic(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, sign.KeyID(p))
		return nil
	}
	return usageError{fmt.Sprintf("unknown command %q", strings.Join(args, " "))}
}

func flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func makeKey(args []string, defaultName string, stdout io.Writer) error {
	fs := flags("new")
	out := fs.String("out", "", "")
	name := fs.String("name", defaultName, "")
	if err := fs.Parse(args); err != nil || *out == "" || *name == "" || fs.NArg() != 0 {
		return usageError{"new needs --out DIR and a --name"}
	}
	if strings.ContainsAny(*name, `/\`) {
		return usageError{"--name is a file name, not a path"}
	}
	keyPath := filepath.Join(*out, *name+".key")
	pubPath := filepath.Join(*out, *name+".pub")
	for _, p := range []string{keyPath, pubPath} {
		if _, err := os.Lstat(p); err == nil {
			return fmt.Errorf("%s exists; refusing to overwrite a key", p)
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	if err := writeNew(keyPath, sign.FormatPrivateKey(priv), 0o600); err != nil {
		return err
	}
	if err := writeNew(pubPath, sign.FormatPublicKey(pub), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s key id %s\nprivate key %s\npublic key  %s\n", *name, sign.KeyID(pub), keyPath, pubPath)
	return nil
}

// writeNew creates path exclusively with mode, whatever the umask.
func writeNew(path, content string, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func readPublic(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := sign.ParsePublicKey(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

func certify(args []string, stdout io.Writer) error {
	fs := flags("certify")
	rootPath := fs.String("root", "", "")
	subjectPath := fs.String("subject", "", "")
	use := fs.String("use", "", "")
	days := fs.Int("days", 0, "")
	out := fs.String("out", "", "")
	if err := fs.Parse(args); err != nil || *rootPath == "" || *subjectPath == "" || *use == "" || *days <= 0 || *out == "" || fs.NArg() != 0 {
		return usageError{"certify needs --root, --subject, --use, --days (positive) and --out"}
	}
	maxV, ok := sign.MaxValidity(sign.Use(*use))
	if !ok {
		return fmt.Errorf("unknown use %q (want manifest, packs, license or license-public)", *use)
	}
	if capDays := int(maxV.Hours() / 24); *days > capDays {
		return fmt.Errorf("use %s is capped at %d days, asked for %d", *use, capDays, *days)
	}
	rawRoot, err := os.ReadFile(*rootPath)
	if err != nil {
		return err
	}
	root, err := sign.ParsePrivateKey(string(rawRoot))
	if err != nil {
		return fmt.Errorf("%s: %w", *rootPath, err)
	}
	subject, err := readPublic(*subjectPath)
	if err != nil {
		return err
	}
	nb := time.Now().UTC().Truncate(time.Second)
	cert, err := sign.Issue(root, subject, sign.Use(*use), nb, nb.AddDate(0, 0, *days))
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return err
	}
	if err := writeNew(*out, string(raw)+"\n", 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "certified %s for %s until %s, issuer %s\n", sign.KeyID(subject), *use,
		nb.AddDate(0, 0, *days).Format(time.RFC3339), sign.KeyID(root.Public().(ed25519.PublicKey)))
	return nil
}

func verify(args []string, stdout io.Writer) error {
	fs := flags("verify")
	rootsDir := fs.String("roots", "", "")
	if err := fs.Parse(args); err != nil || *rootsDir == "" || fs.NArg() != 1 {
		return usageError{"verify needs --roots DIR and one certificate file"}
	}
	names, err := filepath.Glob(filepath.Join(*rootsDir, "*.pub"))
	if err != nil {
		return err
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no .pub files in %s", *rootsDir)
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var cert sign.Certificate
	if err := json.Unmarshal(raw, &cert); err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}
	// The certificate names its own use; verification still checks the
	// signature, use validity and the time window.
	var claimed sign.Body
	if payload, err := decodePayload(cert); err == nil {
		claimed = payload
	}
	for _, n := range names {
		p, err := readPublic(n)
		if err != nil {
			return err
		}
		b, err := cert.Verify([]ed25519.PublicKey{p}, claimed.Use, time.Now())
		if err == nil {
			fmt.Fprintf(stdout, "valid: %s certified for %s until %s, signed by root %s (%s)\n",
				b.KeyID, b.Use, b.NotAfter, sign.KeyID(p), filepath.Base(n))
			return nil
		}
	}
	return fmt.Errorf("%s is not valid against any root in %s", fs.Arg(0), *rootsDir)
}

func decodePayload(c sign.Certificate) (sign.Body, error) {
	var b sign.Body
	raw, err := sign.DecodeB64(c.Payload)
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(raw, &b)
}
