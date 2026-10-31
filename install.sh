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
build=false
web_bundle=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --build) build=true ;;
    --web) shift; web_bundle=${1:?--web needs an absolute built bundle directory} ;;
    *) echo 'Usage: ./install.sh [--build] [--web /absolute/bundle]' >&2; exit 2 ;;
  esac
  shift
done
case "$web_bundle" in ''|/*) ;; *) echo '--web needs an absolute bundle directory' >&2; exit 2 ;; esac
# codexm manages an existing official Codex installation; it never installs or
# replaces codex. Check before creating directories, building or downloading.
codex_binary=$(command -v codex 2>/dev/null || true)
if [ -z "$codex_binary" ]; then
  codex_binary="${CODEX_HOME:-$HOME/.codex}/packages/app-server-daemon/current/bin/codex"
fi
codex_version=$("$codex_binary" --version 2>/dev/null || true)
case "$codex_version" in
  'codex-cli '*) ;;
  *)
    printf '%s\n' 'Codex CLI is required before installing codexm.' \
      'Install the official Codex CLI first: https://developers.openai.com/codex/cli/' \
      'Verify codex --version, then run this installer again.' >&2
    exit 1
    ;;
esac
install_root="${CODEXM_INSTALL_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/codexm}"
link_dir="${CODEXM_LINK_DIR:-$HOME/.local/bin}"
mkdir -p "$install_root/versions" "$install_root/bin" "$link_dir"
stage=$(mktemp -d "$install_root/.install-XXXXXX")
old_current=
old_public=
switched=false
public_switched=false
complete=false
restore_link() {
  restore_target=$1
  restore_value=$2
  if [ -n "$restore_value" ]; then
    ln -s "$restore_value" "$restore_target.rollback-$$"
    case "$target_os" in darwin) mv -fh "$restore_target.rollback-$$" "$restore_target" ;; *) mv -fT "$restore_target.rollback-$$" "$restore_target" ;; esac
  else rm -f "$restore_target"; fi
}
cleanup() {
  if ! $complete; then
    if $public_switched; then restore_link "$link_dir/codexm" "$old_public"; fi
  fi
  if $switched && ! $complete; then
    if [ -n "$old_current" ]; then
      ln -s "$old_current" "$install_root/bin/.rollback-$$"
      case "$target_os" in darwin) mv -fh "$install_root/bin/.rollback-$$" "$install_root/bin/codexm" ;; *) mv -fT "$install_root/bin/.rollback-$$" "$install_root/bin/codexm" ;; esac
    else rm -f "$install_root/bin/codexm"; fi
  fi
  rm -f "$install_root/bin/.codexm-$$" "$link_dir/.codexm-$$"
  rm -rf "$stage"
}
trap cleanup EXIT HUP INT TERM
mkdir "$stage/payload"
case "$build" in
  true) install_mode=source; "$source_root/build.sh" "$target_os" "$target_arch" "$stage/payload/codexm" ;;
  false)
    command -v gh >/dev/null 2>&1 || { echo 'Install gh and sign in, or use ./install.sh --build.' >&2; exit 1; }
    archive="codexm-$target_os-$target_arch.tar.gz"
    gh release download "$release" --repo codeasy-digix/codexm --pattern "$archive" --pattern SHA256SUMS --dir "$stage"
    expected=$(awk -v file="$archive" '$2 == file {print $1}' "$stage/SHA256SUMS")
    test -n "$expected" || { echo 'Release checksum is missing.' >&2; exit 1; }
    if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$stage/$archive" | awk '{print $1}'); else actual=$(shasum -a 256 "$stage/$archive" | awk '{print $1}'); fi
    [ "$actual" = "$expected" ] || { echo 'Release checksum mismatch.' >&2; exit 1; }
    tar -xzf "$stage/$archive" -C "$stage/payload"
    ;;
esac
[ "$("$stage/payload/codexm" --version)" = "codexm $version" ] || { echo 'Installed version would not match.' >&2; exit 1; }
install_version=$release
if [ "$install_mode" = source ]; then
  if command -v sha256sum >/dev/null 2>&1; then binary_id=$(sha256sum "$stage/payload/codexm" | awk '{print $1}'); else binary_id=$(shasum -a 256 "$stage/payload/codexm" | awk '{print $1}'); fi
  install_version="$release-source-$binary_id"
fi
if [ -n "$web_bundle" ]; then
  mkdir "$stage/payload/web"
  web_id=$("$stage/payload/codexm" __install-web "$web_bundle" "$stage/payload/web/bundle")
  install_version="$install_version-web-$web_id"
fi
destination="$install_root/versions/$install_version/$target_os-$target_arch"
if [ -e "$destination" ]; then
  cmp "$stage/payload/codexm" "$destination/codexm" >/dev/null || { echo 'Existing immutable version differs; it was preserved.' >&2; exit 1; }
  if [ -n "$web_bundle" ]; then "$destination/codexm" web verify --bundle "$destination/web/bundle" >/dev/null; fi
else
  mkdir -p "$(dirname -- "$destination")"
  mv "$stage/payload" "$destination"
fi
for entry in "$install_root/bin/codexm" "$link_dir/codexm"; do
  if [ -e "$entry" ] && [ ! -L "$entry" ]; then echo "Preserved non-symlink entrypoint: $entry" >&2; exit 1; fi
done
if [ -L "$install_root/bin/codexm" ]; then old_current=$(readlink "$install_root/bin/codexm"); fi
if [ -L "$link_dir/codexm" ]; then old_public=$(readlink "$link_dir/codexm"); fi
# A failed migration must not switch either public entrypoint.
"$destination/codexm" __migrate-config
ln -s "$destination/codexm" "$install_root/bin/.codexm-$$"
case "$target_os" in darwin) mv -fh "$install_root/bin/.codexm-$$" "$install_root/bin/codexm" ;; *) mv -fT "$install_root/bin/.codexm-$$" "$install_root/bin/codexm" ;; esac
switched=true
ln -s "$install_root/bin/codexm" "$link_dir/.codexm-$$"
case "$target_os" in darwin) mv -fh "$link_dir/.codexm-$$" "$link_dir/codexm" ;; *) mv -fT "$link_dir/.codexm-$$" "$link_dir/codexm" ;; esac
public_switched=true
complete=true
printf 'Installed codexm %s: %s\n' "$version" "$link_dir/codexm"
