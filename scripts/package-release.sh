#!/bin/sh
set -eu
version=${1:?Usage: package-release.sh VERSION OUTPUT_DIRECTORY}
out=${2:?Usage: package-release.sh VERSION OUTPUT_DIRECTORY}
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || { echo 'Expected numeric semantic version' >&2; exit 2; }
grep -Fx "var version = \"$version\"" main.go >/dev/null || { echo 'Source version does not match release version' >&2; exit 1; }
mkdir -p "$out"
out=$(CDPATH= cd "$out" && pwd -P)
test -z "$(find "$out" -mindepth 1 -maxdepth 1 -print -quit)" || { echo 'Output directory must be empty' >&2; exit 1; }
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
git archive HEAD | tar -x -C "$stage"
sh "$stage/scripts/build-release.sh" "$version" "$stage/bin"
# The publishing runner executes the bundled Linux binary and installer.
test "$("$stage/bin/linux-amd64/lsops" --version)" = "lsops $version"
sh "$stage/install.sh" --dir "$stage/smoke-install"
test "$("$stage/smoke-install/lsops" --version)" = "lsops $version"
rm -rf "$stage/smoke-install"
(
 cd "$stage"
 find bin -type f -print | LC_ALL=C sort | xargs sha256sum > SHA256SUMS
 sha256sum --check SHA256SUMS
)
python3 - "$stage" "$out/lsops-$version.zip" <<'PY'
import pathlib, sys, zipfile
root = pathlib.Path(sys.argv[1])
with zipfile.ZipFile(sys.argv[2], "w", zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(root.rglob("*")):
        if path.is_file():
            archive.write(path, "lsops/" + path.relative_to(root).as_posix())
PY
(cd "$out"; sha256sum "lsops-$version.zip" > SHA256SUMS.txt)
