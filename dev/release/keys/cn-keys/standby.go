package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
)

// The standby root leaves the ceremony only sealed under the owner's
// passphrase, printed where anyone can read it: PBKDF2-HMAC-SHA256 (the
// standard library's KDF; scrypt would need x/crypto, which this module
// does not carry) at OWASP's 600,000 iterations, then AES-256-GCM.
const (
	standbyIterations = 600_000
	minPassphrase     = 32
	standbyAAD        = "claudinite-standby-root-v1"
	armorBegin        = "-----BEGIN CLAUDINITE STANDBY ROOT-----"
	armorEnd          = "-----END CLAUDINITE STANDBY ROOT-----"
	passphraseVar     = "CEREMONY_PASSPHRASE"
)

type sealedKey struct {
	V          int    `json:"v"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func checkPassphrase(p string) error {
	if len(p) < minPassphrase {
		return fmt.Errorf("%s must be at least %d characters; the sealed standby root is published", passphraseVar, minPassphrase)
	}
	return nil
}

func standbyAEAD(passphrase string, salt []byte, iterations int) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, passphrase, salt, iterations, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealStandby(k ed25519.PrivateKey, passphrase string) (string, error) {
	if err := checkPassphrase(passphrase); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	aead, err := standbyAEAD(passphrase, salt, standbyIterations)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	raw, err := json.Marshal(sealedKey{
		V: 1, KDF: "pbkdf2-sha256", Iterations: standbyIterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, k.Seed(), []byte(standbyAAD))),
	})
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding.EncodeToString(raw)
	var b strings.Builder
	b.WriteString(armorBegin + "\n")
	for len(enc) > 64 {
		b.WriteString(enc[:64] + "\n")
		enc = enc[64:]
	}
	b.WriteString(enc + "\n" + armorEnd + "\n")
	return b.String(), nil
}

func openStandby(armored, passphrase string) (ed25519.PrivateKey, error) {
	start := strings.Index(armored, armorBegin)
	end := strings.Index(armored, armorEnd)
	if start < 0 || end < start {
		return nil, errors.New("no sealed standby root block found")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(armored[start+len(armorBegin):end]), ""))
	if err != nil {
		return nil, fmt.Errorf("sealed standby root: %w", err)
	}
	var s sealedKey
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("sealed standby root: %w", err)
	}
	if s.V != 1 || s.KDF != "pbkdf2-sha256" || s.Iterations < standbyIterations {
		return nil, fmt.Errorf("sealed standby root: unsupported format v%d %s/%d", s.V, s.KDF, s.Iterations)
	}
	salt, err1 := base64.StdEncoding.DecodeString(s.Salt)
	nonce, err2 := base64.StdEncoding.DecodeString(s.Nonce)
	ct, err3 := base64.StdEncoding.DecodeString(s.Ciphertext)
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("sealed standby root: %w", err)
	}
	aead, err := standbyAEAD(passphrase, salt, s.Iterations)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("sealed standby root: bad nonce")
	}
	seed, err := aead.Open(nil, nonce, ct, []byte(standbyAAD))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("wrong passphrase, or the sealed block was altered")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// standbyDecrypt writes the standby root key file for a recovery run.
func standbyDecrypt(args []string, stdout io.Writer) error {
	fs := flags("standby decrypt")
	in := fs.String("in", "", "")
	out := fs.String("out", "", "")
	if err := fs.Parse(args); err != nil || *in == "" || *out == "" || fs.NArg() != 0 {
		return usageError{"standby decrypt needs --in SEALED and --out KEY; the passphrase comes from $" + passphraseVar}
	}
	armored, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	k, err := openStandby(string(armored), os.Getenv(passphraseVar))
	if err != nil {
		return err
	}
	if err := writeNew(*out, sign.FormatPrivateKey(k), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "standby root key id %s written to %s\n", sign.KeyID(k.Public().(ed25519.PublicKey)), *out)
	return nil
}
