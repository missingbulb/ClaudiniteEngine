package license

import (
	"strings"
	"testing"
)

func TestAGrantVerifiesOnlyForItsOwnItem(t *testing.T) {
	grant := mint(t, map[string]any{"typ": "grant", "issue": 12, "user_id": nil, "nonce": nil})
	if err := VerifyGrant(grant, testRoots(), testNow, 12); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGrant(grant, testRoots(), testNow, 13); err == nil || !strings.Contains(err.Error(), "#12") {
		t.Error("another item's grant:", err)
	}
	session := mint(t, map[string]any{"issue": 12})
	if err := VerifyGrant(session, testRoots(), testNow, 12); err == nil || !strings.Contains(err.Error(), "session") {
		t.Error("a session key is no grant:", err)
	}
	unbound := mint(t, map[string]any{"typ": "grant", "user_id": nil, "nonce": nil})
	if err := VerifyGrant(unbound, testRoots(), testNow, 12); err == nil {
		t.Error("a grant naming no item")
	}
	if err := VerifyGrant([]byte(`{"x":1}`), testRoots(), testNow, 12); err == nil || !strings.Contains(err.Error(), string(ReasonShape)) {
		t.Error(err)
	}
}

type granter struct {
	bearer string
	issue  int
	err    error
}

func (g *granter) ItemGrant(bearer string, issue int) (string, error) {
	g.bearer, g.issue = bearer, issue
	return `{"grant":1}`, g.err
}

func TestTheActionsKeyIsRequestedOncePerProcess(t *testing.T) {
	calls := 0
	once := &ActionsOnce{Request: func() ActionsResult { calls++; return ActionsResult{Cause: CauseNoOIDC} }}
	for range 3 {
		if r := once.Key(); r.Cause != CauseNoOIDC {
			t.Fatal(r)
		}
	}
	if calls != 1 {
		t.Errorf("requested %d times", calls)
	}
}

func TestAGrantIsAskedWithTheRunsKeyAndNeverWithout(t *testing.T) {
	k := minted(t, map[string]any{"typ": "actions", "user_id": nil, "nonce": nil})
	g := &granter{}
	got, err := Grant(g, ActionsResult{Key: &k, Wire: "WIRE"}, 12)
	if err != nil || got != `{"grant":1}` || g.bearer != "WIRE" || g.issue != 12 {
		t.Error(got, err, g)
	}
	g = &granter{}
	if _, err := Grant(g, ActionsResult{Cause: CauseNoOIDC, Detail: "id-token: write is missing"}, 12); err == nil || !strings.Contains(err.Error(), "id-token") || g.issue != 0 {
		t.Error("no key, no grant:", err)
	}
}
