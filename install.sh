#!/bin/sh
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$source_root/../.." && pwd)
target_os=$(uname -s | tr '[:upper:]' '[:lower:]')
target_arch=$(uname -m)
case "$target_arch" in aarch64) target_arch=arm64 ;; x86_64) target_arch=amd64 ;; esac
case "$target_os/$target_arch" in darwin/arm64|darwin/amd64|linux/arm64|linux/amd64) ;; *) echo 'Unsupported platform' >&2; exit 2 ;; esac
release=$(cat "$repo_root/config/home/codexm-release.txt")
expected_version="codexm ${release#codexm-v}"
platform_root="$repo_root/artifacts/$target_os-$target_arch"
current="$platform_root/codexm-current/codexm"
if [ -x "$current" ] && [ "$("$current" --version 2>/dev/null || true)" = "$expected_version" ]; then
  printf '%s\n' "$current"
  exit 0
fi
archive_name="codexm-$target_os-$target_arch.tar.gz"
expected=$(awk -v asset="$archive_name" '$2 == asset {print $1}' "$repo_root/manifests/codexm-assets.sha256")
test -n "$expected" || { echo 'Missing locked codexm archive checksum' >&2; exit 1; }
command -v gh >/dev/null 2>&1 || { echo 'Restore requires gh auth login, or tools/codexm/build.sh with Go.' >&2; exit 1; }
cache_dir="${XDG_CACHE_HOME:-$HOME/.cache}/home/$release"
mkdir -p "$cache_dir" "$platform_root/codexm"
download_dir=$(mktemp -d "$cache_dir/.download-XXXXXX")
stage=$(mktemp -d "$platform_root/codexm/.install-XXXXXX")
trap 'rm -rf "$stage" "$download_dir"' EXIT HUP INT TERM
gh release download "$release" --repo account-main/.home --pattern "$archive_name" --dir "$download_dir"
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$download_dir/$archive_name" | awk '{print $1}'); else actual=$(shasum -a 256 "$download_dir/$archive_name" | awk '{print $1}'); fi
[ "$actual" = "$expected" ] || { echo 'codexm archive checksum mismatch' >&2; exit 1; }
tar -xzf "$download_dir/$archive_name" -C "$stage"
[ -x "$stage/codexm" ] && [ "$("$stage/codexm" --version)" = "$expected_version" ] || { echo 'Incomplete or incompatible codexm archive' >&2; exit 1; }
install_dir="$platform_root/codexm/$release"
if [ -e "$install_dir" ]; then
  cmp "$stage/codexm" "$install_dir/codexm" >/dev/null || { echo 'Existing codexm release differs; preserved for inspection' >&2; exit 1; }
else
  mv "$stage" "$install_dir"
fi
ln -s "codexm/$release" "$platform_root/.codexm-current-$$"
case "$target_os" in darwin) mv -fh "$platform_root/.codexm-current-$$" "$platform_root/codexm-current" ;; *) mv -fT "$platform_root/.codexm-current-$$" "$platform_root/codexm-current" ;; esac
printf '%s\n' "$platform_root/codexm-current/codexm"
