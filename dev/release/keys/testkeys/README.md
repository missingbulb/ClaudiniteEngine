# Development keys

Test fixtures, made with `cn-keys` in the implementing session of #6. This repository is public,
so these private keys are public; nothing a released `cn` trusts chains to them.

- `root.key`, `standby.key`: the development root and standby root. Their public halves are
  `cn/shared/trust/devroots/`, which only a build with the `devroots` tag embeds: the tests, and
  `dev/release/verify/rehearse.sh` through `REHEARSAL=1 BUILD_TAGS=devroots` (`dev/build/gobuild.sh` refuses
  the tag anywhere else). A build without it embeds `cn/shared/trust/roots/`, the key ceremony's roots
  (#5), and `cn/shared/trust/roots_real_test.go` pins their key ids.
- `release.key`, `release.pub`, `release.cert.json`: a development release key and its
  `manifest` certificate, issued by the development root. The tests and the rehearsal pass them
  to `dev/release/create/sign.sh` with `ROOTS=cn/shared/trust/devroots`; against the default `cn/shared/trust/roots` their
  signature fails.

`dev/release/verify/secretscan.sh` fails a built binary that carries any `.key` here.
