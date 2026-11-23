#!/bin/sh
set -eu

if [ "$(uname -s)" != Darwin ]; then
  echo 'The CodexM Web host is for macOS.' >&2
  exit 1
fi
source_root=$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)
install_root=${CODEXM_INSTALL_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/codexm}
mkdir -p "$install_root"
install_root=$(CDPATH= cd -- "$install_root" && pwd -P)
app="$install_root/CodexM Web.app"
host="$app/Contents/MacOS/codexm-web-host"
entry="$install_root/bin/codexm-web"
release="$install_root/bin/codexm-web-release"

verify_app() {
  test ! -L "$app" && test -x "$host" &&
    test "$(plutil -extract CFBundleIdentifier raw -o - "$app/Contents/Info.plist")" = org.codeasy.codexm.WebHost &&
    codesign --verify --strict "$app"
}

case "${1:-prepare}" in
  prepare)
    if [ ! -e "$app" ] && [ ! -L "$app" ]; then
      stage=$(mktemp -d "$install_root/.web-host-XXXXXX")
      trap 'rm -rf "$stage"' EXIT
      trap 'exit 1' HUP INT TERM
      payload="$stage/CodexM Web.app"
      mkdir -p "$payload/Contents/MacOS"
      cargo build --manifest-path "$source_root/Cargo.toml" --locked --release --package codexm-web-host --target-dir "$stage/target"
      install -m 0755 "$stage/target/release/codexm-web-host" "$payload/Contents/MacOS/codexm-web-host"
      install -m 0644 "$source_root/apps/macos/Resources/CodexMWebHost-Info.plist" "$payload/Contents/Info.plist"
      codesign --force --sign - "$payload"
      codesign --verify --strict "$payload"
      test ! -e "$app" && test ! -L "$app"
      mv -h "$payload" "$app"
    fi
    verify_app
    printf '%s\n' "$app"
    ;;
  activate)
    verify_app
    current=$(readlink "$entry")
    if [ "$current" = "$host" ]; then
      test -L "$release" && test -x "$release"
      exit 0
    fi
    case "$current" in "$install_root"/versions/*/codexm) test -x "$current" ;; *) echo 'The existing CodexM web release was not recognized.' >&2; exit 1 ;; esac
    if [ -e "$release" ] || [ -L "$release" ]; then
      test "$(readlink "$release")" = "$current"
    else
      ln -s "$current" "$release"
    fi
    next="$entry.host-$$"
    trap 'rm -f "$next"' EXIT
    trap 'exit 1' HUP INT TERM
    ln -s "$host" "$next"
    test "$(readlink "$entry")" = "$current"
    mv -fh "$next" "$entry"
    ;;
  *) echo 'Usage: install-web-host.sh [prepare | activate]' >&2; exit 2 ;;
esac
