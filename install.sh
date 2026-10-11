#!/bin/sh
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version=$(cat "$source_root/VERSION")
release="v$version"
install_mode=release
target_os=$(uname -s | tr '[:upper:]' '[:lower:]')
target_arch=$(uname -m)
case "$target_arch" in aarch64) target_arch=arm64 ;; x86_64) target_arch=amd64 ;; esac
case "$target_os/$target_arch" in darwin/arm64|darwin/amd64|linux/arm64|linux/amd64) ;; *) echo 'Unsupported platform' >&2; exit 2 ;; esac
install_root="${CODEXM_INSTALL_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/codexm}"
link_dir="${CODEXM_LINK_DIR:-$HOME/.local/bin}"
mkdir -p "$install_root/versions" "$install_root/bin" "$link_dir"
stage=$(mktemp -d "$install_root/.install-XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir "$stage/payload"
case "${1:-}" in
  --build) install_mode=source; "$source_root/build.sh" "$target_os" "$target_arch" "$stage/payload/codexm" ;;
  '')
    command -v gh >/dev/null 2>&1 || { echo 'Install gh and sign in, or use ./install.sh --build.' >&2; exit 1; }
    archive="codexm-$target_os-$target_arch.tar.gz"
    gh release download "$release" --repo codeasy-digix/codexm --pattern "$archive" --pattern SHA256SUMS --dir "$stage"
    expected=$(awk -v file="$archive" '$2 == file {print $1}' "$stage/SHA256SUMS")
    test -n "$expected" || { echo 'Release checksum is missing.' >&2; exit 1; }
    if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$stage/$archive" | awk '{print $1}'); else actual=$(shasum -a 256 "$stage/$archive" | awk '{print $1}'); fi
    [ "$actual" = "$expected" ] || { echo 'Release checksum mismatch.' >&2; exit 1; }
    tar -xzf "$stage/$archive" -C "$stage/payload"
    ;;
  *) echo 'Usage: ./install.sh [--build]' >&2; exit 2 ;;
esac
[ "$("$stage/payload/codexm" --version)" = "codexm $version" ] || { echo 'Installed version would not match.' >&2; exit 1; }
install_version=$release
if [ "$install_mode" = source ]; then
  if command -v sha256sum >/dev/null 2>&1; then binary_id=$(sha256sum "$stage/payload/codexm" | awk '{print $1}'); else binary_id=$(shasum -a 256 "$stage/payload/codexm" | awk '{print $1}'); fi
  install_version="$release-source-$binary_id"
fi
destination="$install_root/versions/$install_version/$target_os-$target_arch"
if [ -e "$destination" ]; then
  cmp "$stage/payload/codexm" "$destination/codexm" >/dev/null || { echo 'Existing immutable version differs; it was preserved.' >&2; exit 1; }
else
  mkdir -p "$(dirname -- "$destination")"
  mv "$stage/payload" "$destination"
fi
ln -s "$destination/codexm" "$install_root/bin/.codexm-$$"
case "$target_os" in darwin) mv -fh "$install_root/bin/.codexm-$$" "$install_root/bin/codexm" ;; *) mv -fT "$install_root/bin/.codexm-$$" "$install_root/bin/codexm" ;; esac
ln -s "$install_root/bin/codexm" "$link_dir/.codexm-$$"
case "$target_os" in darwin) mv -fh "$link_dir/.codexm-$$" "$link_dir/codexm" ;; *) mv -fT "$link_dir/.codexm-$$" "$link_dir/codexm" ;; esac
printf 'Installed codexm %s: %s\n' "$version" "$link_dir/codexm"
