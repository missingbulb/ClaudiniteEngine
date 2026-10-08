package main

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/sign"
)

// The sealed standby block, as the owner pastes it into the root
// environment for a roots run.
const standbySealedVar = "STANDBY_SEALED"

func rootsCmd(args []string, stdout io.Writer) error {
	summary, _, err := summaryFlag("roots", args)
	if err != nil {
		return err
	}
	return roots(os.Getenv(rootSecret), os.Getenv(standbySealedVar), os.Getenv(passphraseVar),
		appender(*summary), stdout, actionsMask(stdout))
}

// roots recovers the public halves of the root and the standby root, from
// ROOT_KEY and, when both are given, the sealed standby block and its
// passphrase. Only public keys and key ids reach the summary; a standby
// that is absent is reported and the root still printed, and one that does
// not open fails the run after the root is printed.
func roots(rootSeed, sealed, passphrase string, summary, log io.Writer, mask masker) error {
	rootSeed = strings.TrimSpace(rootSeed)
	mask(rootSeed)
	mask(passphrase)
	for _, line := range strings.Split(sealed, "\n") {
		if l := strings.TrimSpace(line); l != "" && l != armorBegin && l != armorEnd {
			mask(l)
		}
	}
	root, err := sign.ParsePrivateKey(rootSeed)
	if err != nil {
		return fmt.Errorf("$%s does not hold a private key", rootSecret)
	}
	rootPub := root.Public().(ed25519.PublicKey)

	var b bytes.Buffer
	fmt.Fprintf(&b, "## Root public keys\n\nNothing below is secret.\n\n")
	fmt.Fprintf(&b, "Root, for `cn/shared/trust/roots/root.pub`, key id `%s`:\n\n```\n%s```\n\n",
		sign.KeyID(rootPub), sign.FormatPublicKey(rootPub))

	var standbyErr error
	switch {
	case sealed == "" || passphrase == "":
		var missing []string
		if sealed == "" {
			missing = append(missing, "`"+standbySealedVar+"`")
		}
		if passphrase == "" {
			missing = append(missing, "`"+passphraseVar+"`")
		}
		fmt.Fprintf(&b, "Standby root, for `cn/shared/trust/roots/standby.pub`: missing. Add %s to the `%s` environment and run again.\n\n",
			strings.Join(missing, " and "), rootEnv)
	default:
		standby, err := openStandby(sealed, passphrase)
		if err != nil {
			standbyErr = fmt.Errorf("standby root: %w", err)
			fmt.Fprintf(&b, "Standby root, for `cn/shared/trust/roots/standby.pub`: not recovered: %v.\n\n", err)
			break
		}
		mask(strings.TrimSpace(sign.FormatPrivateKey(standby)))
		standbyPub := standby.Public().(ed25519.PublicKey)
		fmt.Fprintf(&b, "Standby root, for `cn/shared/trust/roots/standby.pub`, key id `%s`:\n\n```\n%s```\n\n",
			sign.KeyID(standbyPub), sign.FormatPublicKey(standbyPub))
	}
	if _, err := summary.Write(b.Bytes()); err != nil {
		return fmt.Errorf("writing the summary: %w", err)
	}
	if standbyErr != nil {
		return standbyErr
	}
	fmt.Fprintf(log, "root %s recovered; see the run summary\n", sign.KeyID(rootPub))
	return nil
}
