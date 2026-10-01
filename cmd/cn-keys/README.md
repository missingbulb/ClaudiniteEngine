# Root key ceremony (#5)

The ceremony runs once, as the `key-ceremony` workflow in this repository, started from the
browser. In one run it makes the root and the standby root, certifies a working key for every
signing use with the root, and stores each where its workflow reads it:

| Key | Stored in |
| --- | --- |
| root | `ROOT_KEY`, ClaudiniteEngine environment `root` |
| standby root | nowhere on GitHub: sealed under your passphrase, in the run summary |
| `manifest` (365 days) | `CN_RELEASE_KEY`, `CN_RELEASE_CERT`, ClaudiniteEngine environment `release` |
| `packs` (90 days) | `CN_PACKS_KEY`, `CN_PACKS_CERT`, ClaudinitePacks environment `release` |
| `license-public` (90 days) | `ISSUING_KEY_PRIVATE`, `ISSUING_KEY_CERT`, ClaudiniteLicenses repository secrets |
| `license` (90 days) | `KEY_ISSUING_KEY_PRIVATE`, `KEY_ISSUING_KEY_CERT`, ClaudiniteLicenses repository secrets |

No private key is printed; the run summary carries the public keys, the certificates and the sealed
standby root.

## The ceremony

1. Create a fine-grained token (GitHub → Settings → Developer settings → Fine-grained tokens),
   owner `missingbulb`, repositories ClaudiniteEngine, ClaudinitePacks and ClaudiniteLicenses,
   permissions **Secrets: read and write**, **Environments: read and write** and
   **Administration: read** (reading an environment's protection needs it), on all three.
2. Have your password manager generate a random passphrase of at least 32 characters and save it
   there. It is all that protects the standby root, whose sealed copy is published.
3. In ClaudiniteEngine → Settings → Environments, create `ceremony` and `root`. Give each
   yourself as a required reviewer (approving your own run is allowed) and Deployment branches set
   to `main` only. Check that ClaudiniteEngine and ClaudinitePacks have a `release` environment.
   The ceremony refuses to run while either environment is missing or unprotected.
4. In the `ceremony` environment add two secrets: `CEREMONY_TOKEN` (the token) and
   `CEREMONY_PASSPHRASE` (the passphrase).
5. Actions → key-ceremony → Run workflow, mode `ceremony`, and approve the run.
6. From the run's summary, copy the block from `BEGIN CLAUDINITE STANDBY ROOT` to
   `END CLAUDINITE STANDBY ROOT` into your password manager beside the passphrase. It is the only
   copy of the standby root. Then delete the workflow run (the run's `…` menu → Delete workflow
   run), so the sealed block stops being public.
7. Delete `CEREMONY_TOKEN` and `CEREMONY_PASSPHRASE` from the `ceremony` environment and revoke
   the token.
8. Comment on #5 that the ceremony ran. The public keys in the summary then replace the
   development roots: ClaudiniteEngine `license/roots/`, ClaudinitePacks `keys/dev/roots/` (what
   `release-packs.yml` passes to `--roots`) and ClaudiniteLicenses `keys/dev/` (its root and both
   development issuing keys, `license-public` and `license`). From the moment the
   run sets the working keys until those land, every Engine release, Packs publish and Licenses
   deploy signs with a chain nothing trusts and fails verification.

A second ceremony refuses once `ROOT_KEY` exists. A run that fails before storing the root can be
run again; it replaces any working key it had already set. If a run dies after `ROOT_KEY` is stored
but before its summary appears, delete `ROOT_KEY` from `root` by hand and run again: the keys
from the lost run are void.

## Rotating the release key

Working keys expire, the release key after a year and the others after 90 days. To replace them,
add a token like step 1's as `CEREMONY_TOKEN` in the `root` environment, run key-ceremony with mode
`rotate` (`uses` empty for all, or a list such as `manifest`), approve it, then delete the token.
The root never leaves its secret.

## Recovering with the standby root

`cn-keys standby decrypt --in SEALED --out standby.key` opens the sealed block with the passphrase
in `CEREMONY_PASSPHRASE`. Every verifier trusts the standby root as it trusts the root.
