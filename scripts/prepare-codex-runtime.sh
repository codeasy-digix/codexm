#!/bin/zsh

set -euo pipefail

ROOT_DIR="${0:A:h:h}"
RUNTIME_VERSION="${CODEX_RUNTIME_VERSION:-0.144.4}"
ARCHITECTURE="$(uname -m)"

case "$ARCHITECTURE" in
  arm64)
    PACKAGE_SUFFIX="darwin-arm64"
    TARGET_TRIPLE="aarch64-apple-darwin"
    EXPECTED_INTEGRITY="6J3g498cM2oA7vYIJhpuGJlnIi/M5JdYmjB5BZ1Of5HQ0ziIlplFSvH801oVy9J5TQFp642ODzOu/ZEokDUXsg=="
    ;;
  x86_64)
    PACKAGE_SUFFIX="darwin-x64"
    TARGET_TRIPLE="x86_64-apple-darwin"
    EXPECTED_INTEGRITY="k1HC8gdbAy+VmMbekYkhM+r+QE2Xfgd67n1VSp94tjz7aXVKoalHcDkdKNM/uUQ8o2tvbiwhHSUftJF8Sm9/Lw=="
    ;;
  *)
    echo "Unsupported macOS architecture: $ARCHITECTURE" >&2
    exit 1
    ;;
esac

if [[ -n "${CODEX_RUNTIME_PATH:-}" ]]; then
  if [[ ! -x "$CODEX_RUNTIME_PATH" ]]; then
    echo "CODEX_RUNTIME_PATH is not executable: $CODEX_RUNTIME_PATH" >&2
    exit 1
  fi
  echo "$CODEX_RUNTIME_PATH"
  exit 0
fi

if (( $+commands[codex] )); then
  LAUNCHER="${commands[codex]}"
  if /usr/bin/file "$LAUNCHER" | /usr/bin/grep -q 'Mach-O'; then
    echo "$LAUNCHER"
    exit 0
  fi

  RESOLVED_LAUNCHER="${LAUNCHER:A}"
  PACKAGE_ROOT="${RESOLVED_LAUNCHER:h:h}"
  NATIVE_CANDIDATE="$PACKAGE_ROOT/node_modules/@openai/codex-$PACKAGE_SUFFIX/vendor/$TARGET_TRIPLE/bin/codex"
  if [[ -x "$NATIVE_CANDIDATE" ]]; then
    echo "$NATIVE_CANDIDATE"
    exit 0
  fi
fi

CACHE_DIR="$ROOT_DIR/.build/codex-runtime/$RUNTIME_VERSION/$TARGET_TRIPLE"
CACHED_RUNTIME="$CACHE_DIR/codex"
if [[ -x "$CACHED_RUNTIME" ]]; then
  echo "$CACHED_RUNTIME"
  exit 0
fi

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/codex-runtime.XXXXXX")"
trap 'rm -rf "$TEMP_DIR"' EXIT
ARCHIVE="$TEMP_DIR/codex-runtime.tgz"
DOWNLOAD_URL="https://registry.npmjs.org/@openai/codex/-/codex-$RUNTIME_VERSION-$PACKAGE_SUFFIX.tgz"

echo "Downloading native Codex runtime $RUNTIME_VERSION ($ARCHITECTURE)…" >&2
/usr/bin/curl --fail --location --retry 3 --output "$ARCHIVE" "$DOWNLOAD_URL"

ACTUAL_INTEGRITY="$(/usr/bin/openssl dgst -sha512 -binary "$ARCHIVE" | /usr/bin/openssl base64 -A)"
if [[ "$ACTUAL_INTEGRITY" != "$EXPECTED_INTEGRITY" ]]; then
  echo "Codex runtime integrity verification failed." >&2
  exit 1
fi

/usr/bin/tar -xzf "$ARCHIVE" -C "$TEMP_DIR"
EXTRACTED_RUNTIME="$TEMP_DIR/package/vendor/$TARGET_TRIPLE/bin/codex"
if [[ ! -x "$EXTRACTED_RUNTIME" ]]; then
  echo "Downloaded package does not contain the expected Codex runtime." >&2
  exit 1
fi

mkdir -p "$CACHE_DIR"
cp "$EXTRACTED_RUNTIME" "$CACHED_RUNTIME"
chmod 755 "$CACHED_RUNTIME"
echo "$CACHED_RUNTIME"
