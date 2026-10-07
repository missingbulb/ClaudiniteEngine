#!/bin/sh
# Waits until registry.npmjs.org serves the tarballs the launcher fetches for
# one release and platform: the manifest package's and the platform package's.
#
#   dev/release/npm-wait.sh --package NAME --version V --platform P [--timeout SECONDS] [--registry URL]
#
# npm acknowledges a publish before its registry serves the tarball, and the
# registry's CDN keeps a miss for minutes, so a look at a launcher's URL
# before npm has the tarball holds every later look on the miss. The wait
# first asks through a URL no one else uses, and looks at the launcher's own
# URLs only once npm answers there. Each look prints its HTTP status and the
# CDN's cache status, so the run records which wait it spent its time in.
set -eu
fail() { echo "npm-wait: $*" >&2; exit 1; }

package='' version='' platform='' timeout=600 registry=https://registry.npmjs.org
while [ $# -gt 0 ]; do
  case $1 in
    --package|--version|--platform|--timeout|--registry)
      [ $# -ge 2 ] || fail "$1 needs a value"
      case $1 in
        --package) package=$2 ;;
        --version) version=$2 ;;
        --platform) platform=$2 ;;
        --timeout) timeout=$2 ;;
        --registry) registry=$2 ;;
      esac
      shift 2 ;;
    *) fail "usage: dev/release/npm-wait.sh --package NAME --version V --platform P [--timeout SECONDS] [--registry URL]" ;;
  esac
done
if [ -z "$package" ] || [ -z "$version" ] || [ -z "$platform" ]; then
  fail "--package, --version and --platform are required"
fi

name=${package#@*/}
urls="$registry/$package/-/$name-$version.tgz $registry/$package-$platform/-/$name-$platform-$version.tgz"
start=$(date +%s)

# look URL: prints "<status> <cache status>" for one HEAD request.
look() {
  curl -sI -o /dev/null -w '%{http_code} %header{cf-cache-status}' "$1" 2>/dev/null || echo "000 -"
}

# served PHASE SUFFIX: 0 once every URL, with SUFFIX appended, answers 200.
served() {
  ok=0
  for u in $urls; do
    answer=$(look "$u$2")
    echo "npm-wait: $1 +$(($(date +%s) - start))s ${u##*/} $answer"
    case $answer in 200\ *) ;; *) ok=1 ;; esac
  done
  return $ok
}

for phase in fresh launcher; do
  until if [ "$phase" = fresh ]; then served fresh "?npm-wait=$(date +%s%N)"; else served launcher ''; fi; do
    if [ $(($(date +%s) - start)) -ge "$timeout" ]; then
      echo "npm still does not serve $package $version for $platform after $timeout seconds ($phase URLs)"
      exit 1
    fi
    sleep 5
  done
done
echo "npm-wait: served after $(($(date +%s) - start))s"
