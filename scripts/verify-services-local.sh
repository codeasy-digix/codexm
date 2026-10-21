#!/bin/sh
# Runs only disposable native conversations, empty logins and a private OS unit.
set -eu
CODEXM_TEST_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$CODEXM_TEST_ROOT"
mkdir -p dist/local-validation
go build -o dist/local-validation/codexm ./cmd/codexm
export CODEXM_BINARY="$CODEXM_TEST_ROOT/dist/local-validation/codexm"
export CODEXM_NATIVE_RUNTIME="${CODEXM_NATIVE_RUNTIME:-$HOME/.codex/packages/app-server-daemon/current/bin/codex}"
export CODEXM_NATIVE_TEST=1 CODEXM_SERVICE_TEST=1 CODEXM_PLUGIN_TEST=1
set +e
go test -race ./internal/supervisor -run 'TestOfficialNativeWorkerLifecycleAndWriterLock|TestOSServiceRestartsAndAdoptsOwnedNativeWorker|TestOfficialCurrentChatRegistrationAndPausedAccountChangePreserveGoal' -count=1 -v
CODEXM_TEST_RESULT=$?
go test -race ./internal/plugin -run '^(TestOfficialPluginDiscoversSettingsEntrypointsAndRegistrationSkill|TestOfficialPromptCommandsPreventInference)$' -count=1 -v
CODEXM_PLUGIN_RESULT=$?
if [ "$CODEXM_PLUGIN_RESULT" -ne 0 ]; then CODEXM_TEST_RESULT="$CODEXM_PLUGIN_RESULT"; fi
printf '%s\n' "$CODEXM_TEST_RESULT" > dist/local-validation/native-services.exit
exit "$CODEXM_TEST_RESULT"
