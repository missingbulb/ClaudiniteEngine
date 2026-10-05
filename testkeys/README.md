# Development keys

Test fixtures, made with `cn-keys` in the implementing session of #6. This repository is public,
so these private keys are public; nothing a released `cn` trusts chains to them.

- `root.key`, `standby.key`: the development root and standby root. Their public halves are
  `shared/trust/devroots/`, which only a build with the `devroots` tag embeds: the tests, and
  `release/rehearse.sh` through `REHEARSAL=1 BUILD_TAGS=devroots` (`release/gobuild.sh` refuses
  the tag anywhere else). A build without it embeds `shared/trust/roots/`, the key ceremony's roots
  (#5), and `shared/trust/roots_real_test.go` pins their key ids.
- `release.key`, `release.pub`, `release.cert.json`: a development release key and its
  `manifest` certificate, issued by the development root. The tests and the rehearsal pass them
  to `release/sign.sh` with `ROOTS=shared/trust/devroots`; against the default `shared/trust/roots` their
  signature fails.

`release/secretscan.sh` fails a built binary that carries any `.key` here.
