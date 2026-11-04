#!/bin/sh
set -eu
app_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source_app=${1:-$app_root/dist/CodexM.app}
[ -d "$source_app" ] || { echo 'Build CodexM.app first; optionally pass its absolute path.' >&2; exit 1; }
codesign --verify --deep --strict "$source_app"
[ "$(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$source_app/Contents/Info.plist")" = 'org.codeasy.CodexUsage' ] || { echo 'Unexpected application identity' >&2; exit 1; }
if pgrep -x CodexUsageMenuBar >/dev/null; then
  echo 'Quit CodexM / Codex Usage from its menu before replacing the menu app. Other Codex applications can keep running.' >&2
  exit 1
fi
install_dir=${CODEXM_APPLICATIONS_DIR:-$HOME/Applications}
backup_root=${XDG_STATE_HOME:-$HOME/.local/state}/codexm/migrations/desktop-$(date -u +%Y%m%dT%H%M%S)-$$
mkdir -p "$install_dir" "$backup_root"
chmod 700 "$backup_root"
stage=$(mktemp -d "$install_dir/.codexm-install.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
/usr/bin/ditto --norsrc "$source_app" "$stage/CodexM.app"
codesign --verify --deep --strict "$stage/CodexM.app"
for name in 'CodexM.app' 'Codex Usage.app'; do
  if [ -e "$install_dir/$name" ]; then
    [ "$(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$install_dir/$name/Contents/Info.plist")" = 'org.codeasy.CodexUsage' ] || { echo "Unrelated app preserved: $name" >&2; exit 1; }
    mv "$install_dir/$name" "$backup_root/$name"
  fi
done
mv "$stage/CodexM.app" "$install_dir/CodexM.app"
printf 'Installed: %s\nBackup: %s\n' "$install_dir/CodexM.app" "$backup_root"
