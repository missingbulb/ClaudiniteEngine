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

No private key is printed; the run summary carries the public keys, the certificates and the sealed
standby root.

## The ceremony

1. Create a fine-grained token (GitHub → Settings → Developer settings → Fine-grained tokens),
   owner `missingbulb`, repositories ClaudiniteEngine, ClaudinitePacks and ClaudiniteLicenses,
   permissions **Secrets: read and write** and **Environments: read and write**.
2. Make up a passphrase of at least 20 characters and save it in your password manager.
3. In ClaudiniteEngine → Settings → Environments, create `ceremony` and `root`, each with yourself
   as a required reviewer, and check that ClaudiniteEngine and ClaudinitePacks have a `release`
   environment.
4. In the `ceremony` environment add two secrets: `CEREMONY_TOKEN` (the token) and
   `CEREMONY_PASSPHRASE` (the passphrase).
5. Actions → key-ceremony → Run workflow, mode `ceremony`, and approve the run.
6. From the run's summary, copy the block from `BEGIN CLAUDINITE STANDBY ROOT` to
   `END CLAUDINITE STANDBY ROOT` into your password manager beside the passphrase. It is the only
   copy of the standby root.
7. Delete `CEREMONY_TOKEN` and `CEREMONY_PASSPHRASE` from the `ceremony` environment and revoke
   the token.
8. Comment on #5 that the ceremony ran. The root and standby public keys in the summary then
   replace the development roots in each repository.

A second ceremony refuses once `ROOT_KEY` exists. A run that fails stores no root and can be run
again; it replaces any working key it had already set.

## Rotating the release key

Working keys expire, the release key after a year and the others after 90 days. To replace them,
add a token like step 1's as `CEREMONY_TOKEN` in the `root` environment, run key-ceremony with mode
`rotate` (`uses` empty for all, or a list such as `manifest`), approve it, then delete the token.
The root never leaves its secret.

## Recovering with the standby root

`cn-keys standby decrypt --in SEALED --out standby.key` opens the sealed block with the passphrase
in `CEREMONY_PASSPHRASE`. Every verifier trusts the standby root as it trusts the root.
