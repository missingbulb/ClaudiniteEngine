// Package license holds the trust store (the embedded root and standby
// root), the license key verifier, and a session's license flow: the state
// file, the background key request, the gate and the notices.
//
// The web request is a repository_dispatch as the person, answered by the
// Claudinite App's check run whose external id is the session's nonce. Its
// HTTP client is Go's default transport, which honours HTTPS_PROXY and
// SSL_CERT_FILE. Run from a Claude Code web VM on 2026-10-01 (#30, live
// gate A), the request crossed the agent proxy with only those two
// variables set, as the VM sets them, and with no GH_TOKEN or GITHUB_TOKEN
// at all: the proxy supplies the person's GitHub credential, so GET /user,
// the dispatch and the check-run reads succeed without a token header.
// The App's slug in the live check run was "claudinite", which AppSlug
// pins; github.com/apps/claudinite itself answers the proxy's 403 there.
//
// The hooks that pass a notice on to Claude answer
// hookSpecificOutput.additionalContext, which PreToolUse, PostToolUse and
// UserPromptSubmit accept (https://code.claude.com/docs/en/hooks). A
// desktop logs in with GitHub's device flow
// (https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app#using-the-device-flow-to-generate-a-user-access-token).
// An Actions run requests its OIDC token from ACTIONS_ID_TOKEN_REQUEST_URL
// with the bearer ACTIONS_ID_TOKEN_REQUEST_TOKEN, which a job gets with
// permissions id-token: write, and reads GITHUB_REPOSITORY_ID and
// GITHUB_REPOSITORY_OWNER_ID, both default variables
// (https://docs.github.com/en/actions/security-for-github-actions/security-hardening-your-deployments/about-security-hardening-with-openid-connect,
// https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/store-information-in-variables#default-environment-variables).
package license
