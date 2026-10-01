package license

// RepoIdentity is the repo a key is used in, as GitHub described it.
type RepoIdentity struct {
	ID         int64  `json:"id"`
	OwnerID    int64  `json:"owner_id"`
	OwnerType  string `json:"owner_type"`
	OwnerLogin string `json:"owner_login"`
	Private    bool   `json:"private"`
}

// Session is what a session key must also name: the user GitHub said the
// session acts as, and the session's own nonce.
type Session struct {
	UserID int64
	Nonce  string
}

// Bind checks a verified key against the repo and, for a session key, the
// session: the repo id first; then, unless the key is unverified (the
// server's fail-open state), the plan against the repo (a public key only
// on a public repo, a private-repo key only on its repo id, a personal,
// organization or internal key only on its owner's repos); then the user
// and the nonce. A failed bind is no key at all. It returns "" on success.
func Bind(key KeyPayload, repo RepoIdentity, s *Session) Cause {
	if key.RepoID != repo.ID {
		return CauseBindRepo
	}
	if key.State != "unverified" {
		switch key.Plan {
		case PlanPublic:
			if repo.Private {
				return CauseBindPlan
			}
		case PlanPersonal, PlanOrganization, PlanInternal:
			if key.OwnerID != repo.OwnerID {
				return CauseBindPlan
			}
		}
	}
	if s != nil {
		if key.UserID == nil || *key.UserID != s.UserID {
			return CauseBindUser
		}
		if key.Nonce != s.Nonce {
			return CauseBindNonce
		}
	}
	return ""
}
