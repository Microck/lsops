#!/bin/sh
set -eu

if [ "$#" -gt 2 ]; then
  printf '%s\n' 'Usage: sh scripts/build-release.sh [VERSION] [OUTPUT_DIRECTORY]' >&2
  exit 2
fi
release_version=${1:-0.5.0}
case "$release_version" in
  ''|*[!A-Za-z0-9._+-]*)
    printf '%s\n' 'Version may contain only letters, digits, dots, underscores, pluses, and hyphens.' >&2
    exit 2
    ;;
esac
release_root=$(CDPATH= cd "$(dirname "$0")/.." && pwd -P)
release_directory=${2:-$release_root/bin}
command -v go >/dev/null 2>&1 || {
  printf '%s\n' 'Go 1.23 or later is required to build releases.' >&2
  exit 1
}
mkdir -p "$release_directory"
release_directory=$(CDPATH= cd "$release_directory" && pwd -P)

for release_target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  release_os=${release_target%/*}
  release_arch=${release_target#*/}
  release_name=lsops
  if [ "$release_os" = windows ]; then
    release_name=lsops.exe
  fi
  release_target_directory=$release_directory/$release_os-$release_arch
  mkdir -p "$release_target_directory"
  printf 'Building lsops %s for %s/%s\n' "$release_version" "$release_os" "$release_arch"
  (
    cd "$release_root"
    GOOS=$release_os GOARCH=$release_arch CGO_ENABLED=0 GOTOOLCHAIN=local \
      GOWORK=off GOPROXY=off GOSUMDB=off \
      go build -trimpath -buildvcs=false -mod=readonly \
        -ldflags "-s -w -X main.version=$release_version" \
        -o "$release_target_directory/$release_name" .
  )
  chmod 0755 "$release_target_directory/$release_name"
done
printf 'Release binaries written to %s\n' "$release_directory"
