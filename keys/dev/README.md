# Development keys

These are development keys, made with `cn-keys` in the implementing session of #6. They sign
nothing a member trusts.

- `root.key`, `standby.key`: the development root and standby root. Their public halves are
  copied into `license/roots/` and embedded in every build until #5.
- `release.key`, `release.cert.json`: a development release key and its `manifest` certificate,
  issued by the development root. `release/build.sh` signs `manifest.json` with them by default.

A stable release must never embed these roots: `go test -tags stable ./license` fails while
`license/roots/*.pub` equals the `.pub` files here. The root key ceremony in #5 replaces
`license/roots/*.pub` and removes this folder.
