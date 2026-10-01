# hello pack

The pack chassis probe. Declare it only on a repo that tests Claudinite itself.

- **Rules** (`RULES.md`): a bullet saying the pack loaded and one naming the 1.1 change, so a
  session shows which version it holds.
- **Skill** (`skills/hello/`): reached when asked to prove the hello pack loaded.
- **Check** (`checks/hello.go`, Go, tags `work` and `world`): a finding while `HELLO_FINDING`
  exists at the repo root, so Stop blocks and `cn check world` fails until it is deleted.
- **Declared checks** (`declared-checks.json`): `hello-declared` (world) finds while
  `HELLO_DECLARED` holds a line; `hello-declared-work` (work) blocks Stop while `HELLO_UNTRACKED`
  is untracked.
