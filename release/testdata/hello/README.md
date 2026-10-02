# hello pack

The pack chassis probe. Declare it only on a repo that tests Claudinite itself.

- **Rules** (`RULES.md`): a bullet saying the pack loaded and one each naming the 1.1, 1.2 and
  1.3 changes, so a session shows which version it holds.
- **Skill** (`skills/hello/`): reached when asked to prove the hello pack loaded.
- **Forced skill** (`skills/hello-guide/`): forces itself on an edit under `HELLO_SCOPED/`, a
  `Bash` command naming `hello-call`, a prompt naming `HELLO PROMPT` and a `Bash` result naming
  `HELLO_RESULT`; the edit or call is held, the prompt or result nudged, until it is loaded.
- **Check** (`checks/hello.go`, Go, tags `work` and `world`): a finding while `HELLO_FINDING`
  exists at the repo root, so Stop blocks and `cn check world` fails until it is deleted.
- **Declared checks** (`declared-checks.json`): `hello-declared` (world) finds while
  `HELLO_DECLARED` holds a line; `hello-declared-work` (work) blocks Stop while `HELLO_UNTRACKED`
  is untracked; `hello-guard` (action) blocks a `Bash` command naming `HELLO_GUARD` before it
  runs, and advises at Stop for each such call the session recorded.
- **Judge** (`checks/judge.go`, Go, tag `pre-tool-use`): `hello-judge` blocks a `Bash` command
  naming `HELLO_JUDGE` before it runs.
- **SDK probes** (`checks/change.go`, `checks/config.go`, Go): `hello-change` (work, advise)
  advises on every file the change adds under `HELLO_CHANGED/`, read through the change's files
  and added lines; `hello-config` (world) finds while the hello entry's config sets `probe: true`.
