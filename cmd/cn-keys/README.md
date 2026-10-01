# Root key ceremony (#5)

The runbook for creating the engine's root and standby root keys and the first release key. The
tool is `cn-keys`, built from this folder; it needs no network and no Go on the ceremony machine.

## Before the ceremony

- [ ] A machine with no network connection (wired unplugged, Wi-Fi off) for the whole ceremony.
- [ ] Two removable drives, labelled `ROOT` and `STANDBY`, plus one for the release key, labelled
      `RELEASE`.
- [ ] A decision on who holds the standby root, and where: a different person or place from the
      root, and neither of them this repository.

## Steps

1. On a networked machine with Go 1.24, from a checkout of this repo, build the tool for the
   ceremony machine's platform (shown for Linux x64; set `GOOS`/`GOARCH` to match):

   ```
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o cn-keys ./cmd/cn-keys
   ```

   Copy `cn-keys` to the ceremony machine.

2. On the ceremony machine, with the network off, create the root on the `ROOT` drive:

   ```
   ./cn-keys root new --out /media/ROOT
   ```

3. Create the standby root on the `STANDBY` drive:

   ```
   ./cn-keys root new --out /media/STANDBY --name standby
   ```

4. Create the first release key on the `RELEASE` drive:

   ```
   ./cn-keys key new --out /media/RELEASE --name release
   ```

5. Certify the release key with the root, for use `manifest`:

   ```
   ./cn-keys certify --root /media/ROOT/root.key --subject /media/RELEASE/release.pub --use manifest --days 365 --out /media/RELEASE/release.cert.json
   ```

6. Check the certificate against the two new roots:

   ```
   mkdir roots && cp /media/ROOT/root.pub /media/STANDBY/standby.pub roots/
   ./cn-keys verify --roots roots /media/RELEASE/release.cert.json
   ```

7. Copy the public halves out, `roots/root.pub` and `roots/standby.pub`, and nothing else from
   `ROOT` or `STANDBY`.

8. On the networked machine, in a branch of this repo, replace the development roots and remove
   the development keys:

   ```
   cp root.pub standby.pub license/roots/
   git rm -r keys/dev
   go test -tags stable ./license
   ```

   The `stable` test passes only once the development roots are gone. Open the PR.

9. Put the release key where the release workflow reads it: in this repo's Settings → Environments
   → `release`, add the secrets `CN_RELEASE_KEY` (the content of `release.key`) and
   `CN_RELEASE_CERT` (the content of `release.cert.json`).

10. Paste the key ids that steps 2 to 5 printed into a comment on #5.

## Storing the keys

- [ ] `ROOT` drive stored offline, apart from `STANDBY`.
- [ ] `STANDBY` drive handed to its holder.
- [ ] `RELEASE` drive wiped once step 9 is done; the secret is the only copy.
- [ ] No private key file was ever on a networked machine or in this repo.

## Rotating the release key

Repeat steps 4 to 6 and 9 with the root drive. The root is needed again only to certify a new
release key or to issue certificates for the `packs`, `license` and `license-public` uses.
