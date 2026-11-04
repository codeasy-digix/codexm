#!/bin/sh
# Assemble only a verified matching native pair. Never publish from this script.
set -eu
[ "$#" -eq 3 ] || { echo 'usage: package-native.sh BUILD_DIRECTORY PLATFORM OUTPUT_DIRECTORY' >&2; exit 2; }
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
build_dir=$(CDPATH= cd -- "$1" && pwd)
platform=$2
case "$platform" in darwin-arm64|darwin-amd64|linux-arm64|linux-amd64) ;; *) echo 'Unsupported platform' >&2; exit 2;; esac
version=$(cat "$root/VERSION")
[ "$("$build_dir/codexm" --version)" = "codexm $version" ] || exit 1
[ "$("$build_dir/codexm-tui" --version)" = "codexm-tui $version" ] || exit 1
mkdir -p "$3"
out=$(CDPATH= cd -- "$3" && pwd)
archive="$out/codexm-$platform.tar.gz"
[ ! -e "$archive" ] || { echo "Existing archive preserved: $archive" >&2; exit 1; }
stage=$(mktemp -d "${TMPDIR:-/tmp}/codexm-package.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
cp "$build_dir/codexm" "$build_dir/codexm-tui" "$root/LICENSE" "$root/README.md" "$root/THIRD_PARTY_NOTICES.txt" "$stage/"
chmod 755 "$stage/codexm" "$stage/codexm-tui"
cd "$stage"
case "$(uname -s)" in
  Darwin) COPYFILE_DISABLE=1 tar --uid 0 --gid 0 --uname root --gname root -czf "$archive" ./* ;;
  *) tar --owner=0 --group=0 --numeric-owner -czf "$archive" ./* ;;
esac
cd "$out"
if command -v sha256sum >/dev/null 2>&1; then sha256sum "$(basename "$archive")"; else shasum -a 256 "$(basename "$archive")"; fi
