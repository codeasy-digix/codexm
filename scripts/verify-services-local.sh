#!/bin/sh
# Runs only disposable native conversations, empty logins and a private OS unit.
set -eu
CODEXM_TEST_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$CODEXM_TEST_ROOT"
mkdir -p dist/local-validation
go build -o dist/local-validation/codexm ./cmd/codexm
export CODEXM_BINARY="$CODEXM_TEST_ROOT/dist/local-validation/codexm"
export CODEXM_NATIVE_RUNTIME="${CODEXM_NATIVE_RUNTIME:-$HOME/.codex/packages/app-server-daemon/current/bin/codex}"
export CODEXM_NATIVE_TEST=1 CODEXM_SERVICE_TEST=1
set +e
go test -race ./internal/supervisor -run 'TestOfficialNativeWorkerLifecycleAndWriterLock|TestOSServiceRestartsAndAdoptsOwnedNativeWorker' -count=1 -v
CODEXM_TEST_RESULT=$?
printf '%s\n' "$CODEXM_TEST_RESULT" > dist/local-validation/native-services.exit
exit "$CODEXM_TEST_RESULT"
