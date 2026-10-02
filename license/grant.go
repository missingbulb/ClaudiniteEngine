package license

import (
	"crypto/ed25519"
	"fmt"
	"time"
)

// Granter is the license server's item-grant route.
type Granter interface {
	ItemGrant(actionsKey string, issue int) (string, error)
}

// Grant asks for issue's item grant with the run's Actions key, and never
// without one.
func Grant(g Granter, key ActionsResult, issue int) (string, error) {
	if key.Key == nil {
		why := string(key.Cause)
		if key.Detail != "" {
			why += ": " + key.Detail
		}
		return "", fmt.Errorf("this run holds no license key (%s), so no item grant can be asked for", why)
	}
	return g.ItemGrant(key.Wire, issue)
}

// VerifyGrant checks an item grant's wire form: a key that verifies against
// roots at now, typed grant, naming issue. A routine session holds nothing
// else to prove the executor handed it this item.
func VerifyGrant(grant []byte, roots []ed25519.PublicKey, now time.Time, issue int) error {
	k, err := VerifyKey(grant, roots, now)
	if err != nil {
		return fmt.Errorf("%s: %w", ReasonOf(err), err)
	}
	if k.Typ != "grant" {
		return fmt.Errorf("a %s key, not an item grant", k.Typ)
	}
	if k.Issue == nil {
		return fmt.Errorf("the grant names no item")
	}
	if *k.Issue != int64(issue) {
		return fmt.Errorf("the grant is for #%d, not #%d", *k.Issue, issue)
	}
	return nil
}
