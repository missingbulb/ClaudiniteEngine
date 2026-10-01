package license

// Cause names why a place holds no usable key. A refusal from the license
// server is its own cause, spelled as the server's reason (no-plan,
// refused-private, workflow-not-pinned, ...); the rest are the binary's.
type Cause string

const (
	CauseAppNotInstalled   Cause = "app-not-installed"
	CauseNoPushAccess      Cause = "no-push-access"
	CauseGitHubUnreachable Cause = "github-unreachable"
	CauseServerUnreachable Cause = "server-unreachable"
	CauseGitHubUser        Cause = "github-user"
	CauseNoGitHubRemote    Cause = "no-github-remote"
	CauseLoginExpired      Cause = "login-expired"
	CauseNoOIDC            Cause = "no-oidc"
	CauseStateFile         Cause = "state-file"
	CauseActions           Cause = "actions-session"
	// CauseActionsEnv is a job whose environment does not name its repository.
	CauseActionsEnv Cause = "actions-env"
	// CauseKeyRefused is a key the verifier refused; the detail names the
	// reason.
	CauseKeyRefused Cause = "key-refused"
	CauseBindRepo   Cause = "bind-repo"
	CauseBindPlan   Cause = "bind-plan"
	CauseBindUser   Cause = "bind-user"
	CauseBindNonce  Cause = "bind-nonce"
)
