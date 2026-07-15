#!/bin/zsh

set -euo pipefail

ROOT_DIR="${0:A:h:h}"
SOURCE_APP="$ROOT_DIR/dist/Codex Usage.app"
INSTALL_DIR="$HOME/Applications"
INSTALLED_APP="$INSTALL_DIR/Codex Usage.app"

"$ROOT_DIR/scripts/build-app.sh"
mkdir -p "$INSTALL_DIR"

# Ask a running copy to quit through the standard application event before an update.
/usr/bin/osascript -e 'tell application id "org.codeasy.CodexUsage" to quit' >/dev/null 2>&1 || true
/usr/bin/ditto --norsrc "$SOURCE_APP" "$INSTALLED_APP"
/usr/bin/open "$INSTALLED_APP"

echo "Installed and opened: $INSTALLED_APP"
