# hello pack

The pack chassis probe. Declare it only on a repo that tests Claudinite itself.

- **Rule** (`RULES.md`): one bullet saying the pack loaded, so a session shows it.
- **Skill** (`skills/hello/`): reached when asked to prove the hello pack loaded.
- **Check** (`checks/hello.go`, Go, tags `work` and `world`): a finding while `HELLO_FINDING`
  exists at the repo root, so Stop blocks and `cn check world` fails until it is deleted.
