#!/bin/sh
set -eu
[ "$#" -eq 2 ] || { echo 'usage: render-formula.sh VERSION SOURCE_SHA256' >&2; exit 2; }
case "$1" in *[!0-9.]*|'') echo 'Invalid version' >&2; exit 2;; esac
case "$2" in *[!a-f0-9]*|'') echo 'Invalid SHA-256' >&2; exit 2;; esac
[ "${#2}" -eq 64 ] || { echo 'Invalid SHA-256 length' >&2; exit 2; }
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
sed -e "s/@VERSION@/$1/g" -e "s/@SHA256@/$2/g" "$root/packaging/homebrew/codexm.rb.in"
