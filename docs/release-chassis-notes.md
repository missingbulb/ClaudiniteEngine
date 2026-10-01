# Release chassis: decisions later chunks build on

Choices made while implementing #6 that the issue left open or that differ from its letter. Each
one binds a later chunk.

1. **The certificate signature covers the decoded body bytes**, `Ed25519("claudinite-cert-v1\n" ||
   body JSON)`, not the base64url string as JWS does. The license Worker must sign and verify the
   same way; `shared/sign/testdata/vectors.json` states it in its header and pins it with cases.
2. **`manifest.json` keeps each binary on one line** in a fixed order
   (`release/releasefiles.Format`). The launcher reads its platform's entry with a line pattern
   once the file's hash matched the pin, so the release workflow must write the manifest with
   `release/manifest write`, never with a generic JSON encoder.
3. **Release key secrets** are named `CN_RELEASE_KEY` and `CN_RELEASE_CERT` in a `release`
   environment (`cmd/cn-keys/README.md`, step 9). `release/sign.sh` (split out of
   `release/build.sh` in #8) reads them through `RELEASE_KEY` and `RELEASE_CERT` file paths and
   falls back to `keys/dev/` with a warning while that folder exists. The dev release
   certificate expires on 2027-10-01.
4. **Launcher exit codes for a refusal** (configuration or hash failure): at SessionStart, a
   `Claudinite refused to run its engine: <reason>: stop and ask the person before continuing.`
   line on stdout and exit 0; in any other hook, exit 2, which blocks; in Actions or for a plain
   command such as `env install`, exit 1. Outages follow the design's table.
5. **The registry stub speaks HTTPS** with a self-signed certificate that curl trusts through
   `CURL_CA_BUNDLE`, so the launcher's `--proto =https` holds in every test as well.
6. **Static linking** is asserted for the Linux binaries only. macOS binaries always link dyld
   and Windows has no equivalent; `release/smoke.sh` checks their executable type instead.
7. **Files beyond the issue's layout:** `release/releasefiles/` (the manifest format, shared by
   the manifest tool, the stub tests and the launcher tests), `release/gobuild.sh` (the one
   per-platform build command, used by `build.sh`, `repro.sh` and the secret-scan test),
   `release/smoke.sh`, `release/repro.sh`, and `probe/desktop-timings/timeit/` (the probe's
   stopwatch, since POSIX `sh` has no sub-second clock). `cn-keys` also has `key new` for
   subject keys and `root new --name` for the standby.
8. **The timing probe reports five rows**: the check-program build is two numbers, cold and warm,
   as the design quotes them.
