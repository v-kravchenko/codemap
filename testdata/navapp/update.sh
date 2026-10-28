#!/usr/bin/env bash
# Compiles the fixture and packs its classes into classes.zip, which navigation_test.go reads; the
# release build's R8 mapping goes to crash/mapping.txt, for the obfuscated crash traces.
# Needs the Android SDK (local.properties or ANDROID_HOME); gw queues the build when it is installed.
set -euo pipefail
cd "$(dirname "$0")"
tasks=(:app:compileDebugKotlin :app:minifyReleaseWithR8 -q)
if command -v gw >/dev/null; then gw "${tasks[@]}"; else ./gradlew "${tasks[@]}"; fi
classes=app/build/intermediates/built_in_kotlinc/debug/compileDebugKotlin/classes
rm -f classes.zip
(cd "$classes" && find . -name '*.class' | sort | zip -q -X -@ "$OLDPWD/classes.zip")
# only the fixture's classes: the libraries' would make it 24 MB
awk '/^#/ && !c { print; next } /^[^ #]/ { c = 1; keep = /^navapp\./ } keep' \
  app/build/intermediates/mapping/release/minifyReleaseWithR8/mapping.txt > crash/mapping.txt
