#!/bin/sh
set -eu

usage() {
  printf '%s\n' \
    'Usage: sh install.sh [--dir DIRECTORY]' \
    'Install the bundled native lsops binary (default: ~/.local/bin).' \
    'If no matching binary is bundled, build locally with Go 1.23 or later.' \
    'No downloads, shell profile changes, or agent settings changes.'
}

die() {
  printf 'lsops install: %s\n' "$*" >&2
  exit 1
}

install_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --dir)
      [ "$#" -ge 2 ] || die '--dir requires a directory'
      [ -n "$2" ] || die '--dir must not be empty'
      install_dir=$2
      shift 2
      ;;
    --dir=*)
      install_dir=${1#--dir=}
      [ -n "$install_dir" ] || die '--dir must not be empty'
      shift
      ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (use --help)" ;;
  esac
done

if [ -z "$install_dir" ]; then
  [ -n "${HOME-}" ] || die 'HOME is unset; choose an installation directory with --dir'
  install_dir=$HOME/.local/bin
fi

source_dir=$(CDPATH= cd "$(dirname "$0")" && pwd -P)
case $(uname -s) in
  Linux) target_os=linux ;;
  Darwin) target_os=darwin ;;
  *) die 'this installer supports Linux and macOS; use install.ps1 on Windows' ;;
esac
case $(uname -m) in
  x86_64|amd64) target_arch=amd64 ;;
  arm64|aarch64) target_arch=arm64 ;;
  *) die 'supported Unix architectures are amd64 and arm64' ;;
esac

source_binary=$source_dir/bin/$target_os-$target_arch/lsops
build_from_source=false
if [ ! -f "$source_binary" ]; then
  command -v go >/dev/null 2>&1 || die "no bundled binary for $target_os-$target_arch; install Go 1.23 or later to build the included source"
  [ -f "$source_dir/go.mod" ] && [ -f "$source_dir/main.go" ] || die 'included Go source is missing'
  build_from_source=true
fi

mkdir -p "$install_dir"
install_dir=$(CDPATH= cd "$install_dir" && pwd -P)
destination=$install_dir/lsops
[ ! -d "$destination" ] || die "destination is a directory: $destination"

temporary_file=
cleanup() {
  if [ -n "$temporary_file" ]; then
    rm -f "$temporary_file"
  fi
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
temporary_file=$(mktemp "$install_dir/.lsops.XXXXXX")

if [ "$build_from_source" = true ]; then
  printf 'No bundled binary for %s-%s; building included source locally.\n' "$target_os" "$target_arch"
  (
    cd "$source_dir"
    GOOS=$target_os GOARCH=$target_arch CGO_ENABLED=0 GOTOOLCHAIN=local \
      GOWORK=off GOPROXY=off GOSUMDB=off \
      go build -trimpath -buildvcs=false -mod=readonly -ldflags '-s -w' \
        -o "$temporary_file" .
  )
else
  cp "$source_binary" "$temporary_file"
fi

chmod 0755 "$temporary_file"
mv -f "$temporary_file" "$destination"
temporary_file=
printf 'Installed lsops: %s\n' "$destination"
case ":${PATH-}:" in
  *":$install_dir:"*) ;;
  *) printf '%s\n' 'This directory is not on PATH. Add it to your shell or agent PATH, or use the full executable path above.' ;;
esac
