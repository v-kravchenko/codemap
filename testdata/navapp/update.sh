#!/usr/bin/env bash
# Compiles the fixture and packs its classes into classes.zip, which navigation_test.go reads.
# Needs the Android SDK (local.properties or ANDROID_HOME); gw queues the build when it is installed.
set -euo pipefail
cd "$(dirname "$0")"
if command -v gw >/dev/null; then gw :app:compileDebugKotlin -q; else ./gradlew :app:compileDebugKotlin -q; fi
classes=app/build/intermediates/built_in_kotlinc/debug/compileDebugKotlin/classes
rm -f classes.zip
(cd "$classes" && find . -name '*.class' | sort | zip -q -X -@ "$OLDPWD/classes.zip")
