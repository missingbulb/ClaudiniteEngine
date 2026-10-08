#!/bin/sh
# Fails if any built binary under DIR carries a secret-shaped string: token
# prefixes, PEM private key blocks, AWS key ids, or any private key under dev/release/keys/testkeys/ in
# base64url or standard base64.
# usage: dev/release/verify/secretscan.sh DIR
set -eu
[ $# -eq 1 ] || { echo "usage: dev/release/verify/secretscan.sh DIR" >&2; exit 2; }
dir=$1
here=$(cd "$(dirname "$0")/../../.." && pwd)

needles=$(mktemp)
strs=$(mktemp)
list=$(mktemp)
trap 'rm -f "$needles" "$strs" "$list"' EXIT
for key in "$here"/dev/release/keys/testkeys/*.key; do
  [ -f "$key" ] || continue
  b64url=$(tr -d '\n' < "$key")
  printf '%s\n' "$b64url" >> "$needles"
  # The same seed in standard base64.
  printf '%s' "$b64url" | tr '_-' '/+' | awk '{ n = length($0) % 4; if (n == 2) $0 = $0 "=="; else if (n == 3) $0 = $0 "="; print }' >> "$needles"
done

find "$dir" -type f -print > "$list"
[ -s "$list" ] || { echo "secretscan: no files under $dir" >&2; exit 1; }
found=0
while IFS= read -r f; do
  strings -n 4 "$f" > "$strs"
  # Report which pattern hit, never the match itself, so a real secret
  # does not land in the CI log.
  for pattern in 'ghp_' 'github_pat_' 'npm_' '-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY-----' 'AKIA[0-9A-Z]{16}'; do
    if grep -Eq -e "$pattern" "$strs"; then
      echo "secretscan: $f carries a string matching $pattern" >&2
      found=1
    fi
  done
  if [ -s "$needles" ] && grep -Fq -f "$needles" "$strs"; then
    echo "secretscan: $f carries a development private key" >&2
    found=1
  fi
done < "$list"
[ "$found" -eq 0 ] || exit 1
echo "secretscan: clean ($(wc -l < "$list" | tr -d ' ') files)"
