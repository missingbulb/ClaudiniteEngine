# dev

Everything that builds, tests, signs and releases `cn`, by stage. The workflows in `.github/workflows/` are entry points: each step is one call into a folder here, so every stage runs the same way on a laptop as in Actions.

| Folder | Stage | Run by |
| --- | --- | --- |
| `build/` | Compile one platform's `cn` as a release does (`gobuild.sh`), the release version (`version.sh`, `next-version.sh`, `major`), the third-party license notices | `create/build.sh`, `release.yml` |
| `test/` | The fast check (`check.sh`: lint, shellcheck, actionlint, short tests), the full tests (`test.sh`), the ported packs against this `cn` (`packs.sh`), the red-nightly and CI-speed issues, the workflow lint (`workflows_test.go`), and `scripttest/`, the helpers every test under `dev/` shares | `ci.yml`, `full.yml`, `ci-speed.yml` |
| `release/create/` | A release in `dist/`: binaries, the manifest, npm packages and tarballs, `SHA256SUMS` (`build.sh`), signing (`sign.sh` with any key, `sign-release.sh` with the real one) | `release.yml`, `full.yml` |
| `release/verify/` | Checks of a built or published release: `smoke.sh`, `repro.sh`, `secretscan.sh`, `sums.sh`, `adoption.sh`, `hop.sh`, the rehearsal (`rehearse.sh`, its `stubs/` and `fixtures/`), `live-packs.sh`, `npm-wait.sh`, and the release-blocker issue a failure opens | `release.yml`, `full.yml`, `live-packs.yml`, `from-npm.yml` |
| `release/publish/` | npm: the publish and its mode and tag, promotion to `latest` (`promote/`, `allowed.sh`, `candidate.sh`, `latest.sh`), hold, revoke and unpublish, the registered canaries | `release.yml`, `promote.yml` |
| `release/` | What every release stage reads: the npm packages, release kinds and dist-tags (`packages.go`, `kind.sh`) and the `pipeline` command the scripts ask | the three stages |
| `release/keys/` | The key ceremony tool and its runbook (`cn-keys/`), and the public development keys only a `devroots` build trusts (`testkeys/`) | `key-ceremony.yml`, the tests and the rehearsal |

Run any script from anywhere; each finds the repository root itself.
