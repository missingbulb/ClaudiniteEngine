#!/bin/sh
# Keeps the build cache ci.yml saves from main to what one run uses, since
# Go only drops an entry five days after its last use and a cache that grows
# with every moved package costs each run's restore and save.
#
# Go marks an entry used by setting its mtime to now, but only once the mtime
# is an hour old, so the restore's mtimes would hide what this run reads:
#
#   dev/test/go-cache.sh age    after the restore: every entry two hours old
#   dev/test/go-cache.sh trim   before the save: drop each entry still that old
set -eu
[ $# -eq 1 ] || { echo "usage: dev/test/go-cache.sh age|trim" >&2; exit 2; }
dir=$(go env GOCACHE)
[ -d "$dir" ] || exit 0
case $1 in
age)
	find "$dir" -mindepth 2 -maxdepth 2 -exec touch -h -d '2 hours ago' {} + ;;
trim)
	before=$(du -sk "$dir" | cut -f1)
	find "$dir" -mindepth 2 -maxdepth 2 -mmin +60 -exec rm -rf {} +
	echo "go cache: ${before} KiB -> $(du -sk "$dir" | cut -f1) KiB" ;;
*)
	echo "usage: dev/test/go-cache.sh age|trim" >&2; exit 2 ;;
esac
