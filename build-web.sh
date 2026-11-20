#!/bin/sh
# Build on the target OS/architecture. Runtime state never enters this bundle.
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
terminal_pty=${WEB_TERMINAL_PTY_PATH:-}
case "$terminal_pty" in
  ""|/*) ;;
  *) echo 'WEB_TERMINAL_PTY_PATH requires an absolute web-terminal-pty path.' >&2; exit 1 ;;
esac
cd "$source_root/web"
case "$(node --version)" in v24.*) ;; *) echo 'Meteor web requires Node 24.' >&2; exit 1 ;; esac
if [ -n "$terminal_pty" ]; then
  node -e 'const p = JSON.parse(require("node:child_process").execFileSync(process.argv[1], ["--version"], { timeout: 5000, maxBuffer: 4096 })); if (p.name !== "web-terminal-pty" || p.protocol !== 1) throw Error("Incompatible terminal helper")' "$terminal_pty"
fi
npm ci
npm run typecheck
npm test
npm run build
cd "$source_root/dist/web/bundle/programs/server"
# Meteor 3.5.2 adds build dependencies after producing its shrinkwrap. Keep a
# reviewed, complete lock with patched deployment dependencies, then use ci.
cp "$source_root/contracts/meteor-3.5.2-server-package.json" package.json.next
mv -f package.json.next package.json
cp "$source_root/contracts/meteor-3.5.2-server-shrinkwrap.json" npm-shrinkwrap.json.next
mv -f npm-shrinkwrap.json.next npm-shrinkwrap.json
npm ci --omit=dev
cd "$source_root"
if [ -n "$terminal_pty" ]; then
  install -d dist/web/bundle/native
  install -m 0755 "$terminal_pty" dist/web/bundle/native/web-terminal-pty
fi
go run ./cmd/webbundle seal dist/web/bundle
