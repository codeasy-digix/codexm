#!/bin/sh
# Build on the target OS/architecture. Runtime state never enters this bundle.
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$source_root/web"
case "$(node --version)" in v24.*) ;; *) echo 'Meteor web requires Node 24.' >&2; exit 1 ;; esac
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
go run ./cmd/webbundle seal dist/web/bundle
