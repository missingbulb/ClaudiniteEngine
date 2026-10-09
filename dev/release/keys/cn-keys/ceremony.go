package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
)

// workingKey is one key a workflow signs with, and the secrets that hold it.
type workingKey struct {
	Use        sign.Use
	Days       int
	Repo       string
	Env        string // "" is a repository-level secret
	KeySecret  string
	CertSecret string
}

var workingKeys = []workingKey{
	{sign.UseManifest, 365, "missingbulb/ClaudiniteEngine", "release", "CN_RELEASE_KEY", "CN_RELEASE_CERT"},
	{sign.UsePacks, 90, "missingbulb/ClaudinitePacks", "release", "CN_PACKS_KEY", "CN_PACKS_CERT"},
	{sign.UseLicensePublic, 90, "missingbulb/ClaudiniteLicenses", "", "ISSUING_KEY_PRIVATE", "ISSUING_KEY_CERT"},
	{sign.UseLicense, 90, "missingbulb/ClaudiniteLicenses", "", "KEY_ISSUING_KEY_PRIVATE", "KEY_ISSUING_KEY_CERT"},
}

// The root key's home: an environment secret of the engine repo, readable
// only by a job in that environment, which is the rotate mode.
const (
	rootRepo    = "missingbulb/ClaudiniteEngine"
	rootEnv     = "root"
	rootSecret  = "ROOT_KEY"
	ceremonyEnv = "ceremony"
)

// rootsToSwap is what must change before anything the new working keys sign
// verifies.
var rootsToSwap = strings.ReplaceAll(`### Roots to swap

Until these land, every Engine release, Packs publish and Licenses deploy fails verification:
the working keys are certified by roots nothing trusts yet.

- ClaudiniteEngine 'cn/packaging/trust/roots/root.pub' and 'cn/packaging/trust/roots/standby.pub', and the key ids 'cn/packaging/trust/roots_real_test.go' pins.
- ClaudinitePacks 'keys/roots/', the directory 'release-packs.yml' passes to '--roots'.
- ClaudiniteLicenses 'packages/signing/roots/', the key Worker's 'TRUST_ROOTS'.
`, "'", "`")

// masker hides a secret value from the Actions log before anything could
// print it.
type masker func(string)

func actionsMask(stdout io.Writer) masker {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return func(string) {}
	}
	return func(v string) {
		if v != "" {
			fmt.Fprintf(stdout, "::add-mask::%s\n", v)
		}
	}
}

func summaryFlag(name string, args []string) (*string, *string, error) {
	fs := flags(name)
	summary := fs.String("summary", "", "")
	uses := fs.String("use", "", "")
	if err := fs.Parse(args); err != nil || *summary == "" || fs.NArg() != 0 {
		return nil, nil, usageError{name + " needs --summary FILE"}
	}
	if name != "rotate" && *uses != "" {
		return nil, nil, usageError{name + " takes no --use; --use is for rotate"}
	}
	return summary, uses, nil
}

func ceremonyCmd(args []string, stdout io.Writer) error {
	summary, _, err := summaryFlag("ceremony", args)
	if err != nil {
		return err
	}
	return ceremony(ghCLI{}, os.Getenv(passphraseVar), time.Now(), appender(*summary), stdout, actionsMask(stdout))
}

func rotateCmd(args []string, stdout io.Writer) error {
	summary, uses, err := summaryFlag("rotate", args)
	if err != nil {
		return err
	}
	root, err := sign.ParsePrivateKey(os.Getenv(rootSecret))
	if err != nil {
		return fmt.Errorf("$%s: %w", rootSecret, err)
	}
	keys, err := selectKeys(*uses)
	if err != nil {
		return err
	}
	return rotate(root, keys, ghCLI{}, time.Now(), appender(*summary), stdout, actionsMask(stdout))
}

// appender opens the step summary for one append per write.
type appender string

func (a appender) Write(p []byte) (int, error) {
	f, err := os.OpenFile(string(a), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := f.Write(p)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func selectKeys(list string) ([]workingKey, error) {
	if list == "" {
		return workingKeys, nil
	}
	var out []workingKey
	for _, u := range strings.Split(list, ",") {
		i := slices.IndexFunc(workingKeys, func(k workingKey) bool { return string(k.Use) == strings.TrimSpace(u) })
		if i < 0 {
			return nil, fmt.Errorf("no working key has use %q", u)
		}
		out = append(out, workingKeys[i])
	}
	return out, nil
}

func checkTargets(gh secretStore, keys []workingKey) error {
	if err := gh.CheckAuth(); err != nil {
		return err
	}
	for _, k := range keys {
		e, err := gh.Environment(k.Repo, k.Env)
		if err != nil {
			return err
		}
		if e == nil && k.Env != "" {
			return fmt.Errorf("%s has no environment %q; create it under Settings → Environments", k.Repo, k.Env)
		}
		if e == nil {
			return fmt.Errorf("the token cannot reach %s", k.Repo)
		}
	}
	return nil
}

// requireProtected refuses an environment that is missing, that runs
// without a required reviewer, or that any branch may deploy from.
func requireProtected(gh secretStore, repo, env string) error {
	e, err := gh.Environment(repo, env)
	if err != nil {
		return err
	}
	switch {
	case e == nil:
		return fmt.Errorf("%s has no environment %q; create it with yourself as a required reviewer (dev/release/keys/cn-keys/README.md)", repo, env)
	case !e.RequiredReviewers:
		return fmt.Errorf("%s environment %q has no required reviewer; add yourself before running", repo, env)
	case !e.BranchPolicy:
		return fmt.Errorf("%s environment %q allows every branch; limit its deployment branches to main", repo, env)
	}
	return nil
}

// ceremony makes the root and standby root and certifies a fresh key for
// every working use with the root, all in memory. Working keys go to their
// secrets, the root to ROOT_KEY in the root environment last, which is what
// makes a second ceremony refuse; the standby root leaves only sealed under
// the passphrase, in the summary.
func ceremony(gh secretStore, passphrase string, now time.Time, summary, log io.Writer, mask masker) error {
	mask(passphrase)
	if err := checkPassphrase(passphrase); err != nil {
		return err
	}
	if err := checkTargets(gh, workingKeys); err != nil {
		return err
	}
	for _, env := range []string{ceremonyEnv, rootEnv} {
		if err := requireProtected(gh, rootRepo, env); err != nil {
			return err
		}
	}
	names, err := gh.SecretNames(rootRepo, rootEnv)
	if err != nil {
		return fmt.Errorf("cannot list the secrets of %s environment %s: %w", rootRepo, rootEnv, err)
	}
	if slices.Contains(names, rootSecret) {
		return fmt.Errorf("%s environment %s already holds %s: the ceremony has run; use the rotate mode", rootRepo, rootEnv, rootSecret)
	}

	rootPub, root, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	standbyPub, standby, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	rootSeed := strings.TrimSpace(sign.FormatPrivateKey(root))
	mask(rootSeed)
	mask(strings.TrimSpace(sign.FormatPrivateKey(standby)))
	sealed, err := sealStandby(standby, passphrase)
	if err != nil {
		return err
	}

	issued, err := issueAll(root, workingKeys, gh, now, mask)
	if err != nil {
		return fmt.Errorf("%w\nno root was stored; run the ceremony again, which replaces any secret already set", err)
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "## Root key ceremony\n\nNothing below is secret: the standby root is sealed under your passphrase.\n\n")
	fmt.Fprintf(&b, "Root, for `cn/packaging/trust/roots/root.pub`, key id `%s`, stored as `%s` in %s environment `%s`:\n\n```\n%s```\n\n",
		sign.KeyID(rootPub), rootSecret, rootRepo, rootEnv, sign.FormatPublicKey(rootPub))
	fmt.Fprintf(&b, "Standby root, for `cn/packaging/trust/roots/standby.pub`, key id `%s`:\n\n```\n%s```\n\n",
		sign.KeyID(standbyPub), sign.FormatPublicKey(standbyPub))
	writeIssued(&b, issued)
	fmt.Fprintf(&b, "### Sealed standby root\n\nSave this whole block in your password manager, beside the passphrase. "+
		"It is the only copy of the standby root.\n\n```\n%s```\n\n", sealed)
	fmt.Fprintf(&b, "### Now\n\n1. Save the sealed block above in your password manager, beside the passphrase.\n"+
		"2. Comment on #5 that the ceremony ran, pasting the root and standby root public keys and key ids above: "+
		"they are public, and this summary goes with the run.\n"+
		"3. Delete this workflow run, so the sealed block stops being public.\n"+
		"4. Delete the `ceremony` environment's secrets `CEREMONY_TOKEN` and `%s`, then revoke the token.\n\n", passphraseVar)
	fmt.Fprint(&b, rootsToSwap)
	// The summary carries the only copy of the sealed standby, so it is
	// written before the root is stored: a run that cannot report stores no
	// root and can simply run again.
	if _, err := summary.Write(b.Bytes()); err != nil {
		return fmt.Errorf("writing the summary: %w; no root was stored, run the ceremony again", err)
	}

	if err := gh.SetSecret(rootRepo, rootEnv, rootSecret, []byte(rootSeed)); err != nil {
		return storeRootFailed(summary, err)
	}
	fmt.Fprintf(log, "ceremony done: root %s, standby %s; see the run summary\n", sign.KeyID(rootPub), sign.KeyID(standbyPub))
	return nil
}

func storeRootFailed(summary io.Writer, err error) error {
	_, _ = fmt.Fprintf(summary, "\n**The root could not be stored. Discard everything above and run the ceremony again.**\n")
	return fmt.Errorf("storing %s in %s environment %s: %w; run the ceremony again", rootSecret, rootRepo, rootEnv, err)
}

// rotate replaces the given working keys, certified by root.
func rotate(root ed25519.PrivateKey, keys []workingKey, gh secretStore, now time.Time, summary, log io.Writer, mask masker) error {
	if err := checkTargets(gh, keys); err != nil {
		return err
	}
	if err := requireProtected(gh, rootRepo, rootEnv); err != nil {
		return err
	}
	issued, err := issueAll(root, keys, gh, now, mask)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "## Working key rotation\n\nCertified by root `%s`.\n\n", sign.KeyID(root.Public().(ed25519.PublicKey)))
	writeIssued(&b, issued)
	if _, err := summary.Write(b.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(log, "rotated %d working keys; see the run summary\n", len(issued))
	return nil
}

type issuedKey struct {
	workingKey
	Public ed25519.PublicKey
	Cert   []byte
	Until  time.Time
}

// issueAll keeps each working private key in memory only, from generation
// until it is stored in its secret.
func issueAll(root ed25519.PrivateKey, keys []workingKey, gh secretStore, now time.Time, mask masker) ([]issuedKey, error) {
	nb := now.UTC().Truncate(time.Second)
	var out []issuedKey
	for _, k := range keys {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		seed := strings.TrimSpace(sign.FormatPrivateKey(priv))
		mask(seed)
		na := nb.AddDate(0, 0, k.Days)
		cert, err := sign.Issue(root, pub, k.Use, nb, na)
		if err != nil {
			return nil, err
		}
		certJSON, err := json.Marshal(cert)
		if err != nil {
			return nil, err
		}
		if err := gh.SetSecret(k.Repo, k.Env, k.KeySecret, []byte(seed)); err != nil {
			return nil, setFailed(out, k, err)
		}
		if err := gh.SetSecret(k.Repo, k.Env, k.CertSecret, certJSON); err != nil {
			return nil, setFailed(out, k, err)
		}
		out = append(out, issuedKey{k, pub, certJSON, na})
	}
	return out, nil
}

func setFailed(done []issuedKey, k workingKey, err error) error {
	set := "none"
	if len(done) > 0 {
		var names []string
		for _, d := range done {
			names = append(names, d.KeySecret, d.CertSecret)
		}
		set = strings.Join(names, ", ")
	}
	return errors.Join(fmt.Errorf("storing the %s key in %s: %w", k.Use, where(k), err),
		fmt.Errorf("secrets already set: %s", set))
}

func where(k workingKey) string {
	if k.Env == "" {
		return k.Repo + " (repository secrets)"
	}
	return k.Repo + " environment `" + k.Env + "`"
}

func writeIssued(w io.Writer, issued []issuedKey) {
	fmt.Fprintf(w, "### Working keys\n\n")
	for _, k := range issued {
		fmt.Fprintf(w, "- `%s` key `%s`, valid until %s, stored in %s as `%s` and `%s`; public key `%s`, certificate:\n\n  ```\n  %s\n  ```\n\n",
			k.Use, sign.KeyID(k.Public), k.Until.Format(time.RFC3339), where(k.workingKey), k.KeySecret, k.CertSecret,
			strings.TrimSpace(sign.FormatPublicKey(k.Public)), k.Cert)
	}
}
