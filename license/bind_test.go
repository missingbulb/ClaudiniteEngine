package license

import "testing"

var publicRepo = RepoIdentity{ID: 11, OwnerID: 3, OwnerType: "User", OwnerLogin: "acme"}

func session() *Session { return &Session{UserID: 7, Nonce: "nonce-0123456789ab"} }

func TestBind(t *testing.T) {
	private := publicRepo
	private.Private = true
	otherOwner := publicRepo
	otherOwner.OwnerID = 4
	for _, c := range []struct {
		name string
		edit map[string]any
		repo RepoIdentity
		s    *Session
		want Cause
	}{
		{"public key on a public repo", nil, publicRepo, session(), ""},
		{"another repo's key", map[string]any{"repo_id": 12}, publicRepo, session(), CauseBindRepo},
		{"public key on a private repo", nil, private, session(), CauseBindPlan},
		{"unverified public key on a private repo", map[string]any{"state": "unverified"}, private, session(), ""},
		{"unverified private-repo key on a private repo", map[string]any{"state": "unverified", "plan": "private-repo"}, private, session(), ""},
		{"unverified key for another repo", map[string]any{"state": "unverified", "repo_id": 12}, private, session(), CauseBindRepo},
		{"private-repo key on its repo", map[string]any{"plan": "private-repo"}, private, session(), ""},
		{"personal key on its owner's repo", map[string]any{"plan": "personal"}, private, session(), ""},
		{"personal key on another owner's repo", map[string]any{"plan": "personal"}, otherOwner, session(), CauseBindPlan},
		{"organization key on another owner's repo", map[string]any{"plan": "organization"}, otherOwner, session(), CauseBindPlan},
		{"internal key on another owner's repo", map[string]any{"plan": "internal"}, otherOwner, session(), CauseBindPlan},
		{"unverified personal key on another owner's repo", map[string]any{"plan": "personal", "state": "unverified"}, otherOwner, session(), ""},
		{"another user's key", map[string]any{"user_id": 8}, publicRepo, session(), CauseBindUser},
		{"a key with no user in a session", map[string]any{"user_id": nil}, publicRepo, session(), CauseBindUser},
		{"another session's nonce", map[string]any{"nonce": "nonce-other-000000"}, publicRepo, session(), CauseBindNonce},
		{"unverified with another nonce", map[string]any{"state": "unverified", "nonce": "nonce-other-000000"}, publicRepo, session(), CauseBindNonce},
		{"an actions key has no session", map[string]any{"typ": "actions", "user_id": nil, "nonce": nil}, publicRepo, nil, ""},
	} {
		k := minted(t, c.edit)
		if got := Bind(k, c.repo, c.s); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
